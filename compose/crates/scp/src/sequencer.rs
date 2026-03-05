use std::sync::Mutex;

use compose_spec::{ChainId, DecisionState, Instance, StateRoot};
use thiserror::Error;
use tracing::{info, warn};

use crate::mailbox::{MailboxMessage, MailboxMessageHeader};

/// Errors returned by [`SequencerInstance`] operations.
#[derive(Debug, Error)]
pub enum SequencerError {
    #[error("no transactions to execute")]
    NoTransactions,
    #[error("sequencer not in simulating state")]
    NotInSimulatingState,
    #[error("simulating sequencer failed: {0}")]
    SimulationFailed(Box<dyn std::error::Error + Send + Sync>),
}

/// State machine for a sequencer in an SCP session.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SequencerState {
    Simulating,
    WaitingDecided,
    Done,
}

/// Inputs for the mailbox-aware simulator.
#[derive(Debug, Clone)]
pub struct SimulationRequest {
    pub put_inbox_messages: Vec<MailboxMessage>,
    pub transactions: Vec<Vec<u8>>,
    pub snapshot: StateRoot,
}

/// Result of a simulation step.
#[derive(Debug)]
pub struct SimulationOutput {
    /// If there is a read miss, this is populated with the expected header.
    pub read_request: Option<MailboxMessageHeader>,
    /// Mailbox messages written by the simulation.
    pub write_messages: Vec<MailboxMessage>,
}

/// Execution engine (e.g. EVM) for running transaction simulations.
pub trait ExecutionEngine: Send + Sync {
    fn chain_id(&self) -> ChainId;
    /// Runs the VM with a tracer for the simulation request.
    fn simulate(
        &self,
        request: SimulationRequest,
    ) -> Result<SimulationOutput, Box<dyn std::error::Error + Send + Sync>>;
}

/// Network interface for the sequencer side of an SCP instance.
pub trait SequencerNetwork: Send + Sync {
    fn send_mailbox_message(&self, recipient: ChainId, msg: MailboxMessage);
    fn send_vote(&self, vote: bool);
}

struct SequencerInner {
    state: SequencerState,
    decision_state: DecisionState,
    txs: Vec<Vec<u8>>,
    expected_read_requests: Vec<MailboxMessageHeader>,
    pending_messages: Vec<MailboxMessage>,
    put_inbox_messages: Vec<MailboxMessage>,
    vm_snapshot: StateRoot,
    written_messages_cache: Vec<MailboxMessage>,
}

/// Sequencer-side logic for a single SCP instance.
pub struct SequencerInstance<E: ExecutionEngine, N: SequencerNetwork> {
    inner: Mutex<SequencerInner>,
    execution: E,
    network: N,
}

impl<E: ExecutionEngine, N: SequencerNetwork> std::fmt::Debug for SequencerInstance<E, N> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("SequencerInstance").finish_non_exhaustive()
    }
}

impl<E: ExecutionEngine, N: SequencerNetwork> SequencerInstance<E, N> {
    pub fn new(
        instance: &Instance,
        execution: E,
        network: N,
        vm_snapshot: StateRoot,
    ) -> Result<Self, SequencerError> {
        let chain_id = execution.chain_id();
        let mut txs = Vec::new();

        for req in &instance.xt_request.transactions {
            if req.chain_id != chain_id {
                continue;
            }
            for payload in &req.transactions {
                txs.push(payload.clone());
            }
        }

        if txs.is_empty() {
            return Err(SequencerError::NoTransactions);
        }

        Ok(Self {
            inner: Mutex::new(SequencerInner {
                state: SequencerState::Simulating,
                decision_state: DecisionState::Pending,
                txs,
                expected_read_requests: Vec::new(),
                pending_messages: Vec::new(),
                put_inbox_messages: Vec::new(),
                vm_snapshot,
                written_messages_cache: Vec::new(),
            }),
            execution,
            network,
        })
    }

    #[must_use]
    pub fn decision_state(&self) -> DecisionState {
        self.inner.lock().unwrap().decision_state
    }

    /// Returns the current sequencer state (for testing).
    #[must_use]
    pub fn state(&self) -> SequencerState {
        self.inner.lock().unwrap().state
    }

    /// Returns the number of expected read requests (for testing).
    #[must_use]
    pub fn expected_read_requests(&self) -> Vec<MailboxMessageHeader> {
        self.inner.lock().unwrap().expected_read_requests.clone()
    }

    /// Runs the simulation loop. On success, sends `Vote(true)`.
    /// On read miss, waits for mailbox messages and re-simulates.
    /// On error, sends `Vote(false)`.
    pub fn run(&self) -> Result<(), SequencerError> {
        let mut inner = self.inner.lock().unwrap();
        if inner.state != SequencerState::Simulating {
            return Err(SequencerError::NotInSimulatingState);
        }

        let request = SimulationRequest {
            put_inbox_messages: inner.put_inbox_messages.clone(),
            transactions: inner.txs.clone(),
            snapshot: inner.vm_snapshot,
        };

        // Simulate while holding the lock, preventing concurrent timeout/decided
        // from mutating state mid-simulation.
        let output = match self.execution.simulate(request) {
            Ok(out) => out,
            Err(e) => {
                info!("Simulation failed, rejecting instance. Error: {e}");
                self.network.send_vote(false);
                inner.state = SequencerState::Done;
                inner.decision_state = DecisionState::Rejected;
                return Err(SequencerError::SimulationFailed(e));
            }
        };

        // Send write messages
        Self::send_write_messages(&self.network, &mut inner, output.write_messages);

        if let Some(read_request) = output.read_request {
            info!(
                source_chain_id = read_request.source_chain_id.get(),
                label = %read_request.label,
                "Simulation hit read miss, requesting mailbox message"
            );
            inner.expected_read_requests.push(read_request);
            drop(inner);
            return self.consume_received_and_simulate();
        }

        // Vote true
        info!("Simulation succeeded, voting true");
        self.network.send_vote(true);
        inner.state = SequencerState::WaitingDecided;
        Ok(())
    }

    fn send_write_messages(network: &N, inner: &mut SequencerInner, messages: Vec<MailboxMessage>) {
        for msg in messages {
            let already_sent = inner.written_messages_cache.contains(&msg);
            if already_sent {
                continue;
            }
            network.send_mailbox_message(msg.header.dest_chain_id, msg.clone());
            inner.written_messages_cache.push(msg);
        }
    }

    /// Checks if any expected read mailbox messages have been received.
    /// If so, removes from the lists and calls `run` to re-simulate.
    fn consume_received_and_simulate(&self) -> Result<(), SequencerError> {
        let mut inner = self.inner.lock().unwrap();
        let mut included_any = false;

        let mut idx = 0;
        while idx < inner.expected_read_requests.len() {
            let expected = &inner.expected_read_requests[idx];
            let mut matched = false;

            for recv_idx in 0..inner.pending_messages.len() {
                if inner.pending_messages[recv_idx].header == *expected {
                    let msg = inner.pending_messages.remove(recv_idx);
                    inner.put_inbox_messages.push(msg);
                    inner.expected_read_requests.remove(idx);
                    included_any = true;
                    matched = true;
                    break;
                }
            }

            if !matched {
                idx += 1;
            }
        }

        drop(inner);

        if included_any {
            info!("Consuming mailbox messages and re-simulating");
            return self.run();
        }
        Ok(())
    }

    /// Processes an incoming mailbox message from another sequencer.
    pub fn process_mailbox_message(&self, msg: MailboxMessage) -> Result<(), SequencerError> {
        let mut inner = self.inner.lock().unwrap();
        if inner.state != SequencerState::Simulating {
            info!(
                source_chain_id = msg.header.source_chain_id.get(),
                label = %msg.header.label,
                "Ignoring mailbox message because not in simulating state"
            );
            return Ok(());
        }

        info!(
            source_chain_id = msg.header.source_chain_id.get(),
            label = %msg.header.label,
            "Adding mailbox message to pending list"
        );
        inner.pending_messages.push(msg);
        drop(inner);
        self.consume_received_and_simulate()
    }

    /// Processes a decided message from the SP.
    pub fn process_decided_message(&self, decided: bool) {
        let mut inner = self.inner.lock().unwrap();

        if inner.state == SequencerState::Done {
            info!(
                received_decided = decided,
                stored_decision = %inner.decision_state,
                "Ignoring decided message because already done"
            );
            return;
        }

        info!(
            received_decided = decided,
            "Processing decided message from SP"
        );
        inner.state = SequencerState::Done;
        inner.decision_state = if decided {
            DecisionState::Accepted
        } else {
            DecisionState::Rejected
        };
    }

    /// Invoked when the timer fires. If still simulating, rejects and sends `Vote(false)`.
    pub fn timeout(&self) {
        let mut inner = self.inner.lock().unwrap();

        if inner.state == SequencerState::WaitingDecided || inner.state == SequencerState::Done {
            info!("Ignoring timeout because already waiting for decided or done");
            return;
        }

        for req in &inner.expected_read_requests {
            warn!(
                op = "read",
                src_chain = req.source_chain_id.get(),
                dest_chain = req.dest_chain_id.get(),
                sender = %req.sender,
                receiver = %req.receiver,
                session_id = req.session_id.get(),
                label = %req.label,
                "Unfulfilled mailbox request"
            );
        }

        info!(
            unfulfilled_reads = inner.expected_read_requests.len(),
            "Timeout occurred, rejecting instance"
        );

        inner.state = SequencerState::Done;
        inner.decision_state = DecisionState::Rejected;
        self.network.send_vote(false);
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use compose_spec::{TransactionRequest, XtRequest};

    use super::*;

    fn make_msg(src: u64, label: &str, data: &[u8]) -> MailboxMessage {
        MailboxMessage {
            header: MailboxMessageHeader {
                source_chain_id: ChainId(src),
                dest_chain_id: ChainId(1),
                sender: EthAddress([1; 20]),
                receiver: EthAddress([2; 20]),
                session_id: SessionId(1),
                label: label.to_string(),
            },
            data: data.to_vec(),
        }
    }

    use compose_spec::{EthAddress, SessionId};

    struct SimStep {
        read: Option<MailboxMessageHeader>,
        write: Vec<MailboxMessage>,
        err: Option<Box<dyn std::error::Error + Send + Sync>>,
    }

    struct FakeEngine {
        id: ChainId,
        steps: Mutex<Vec<SimStep>>,
        call_count: Mutex<usize>,
        last_req: Mutex<Option<SimulationRequest>>,
    }

    impl FakeEngine {
        fn new(id: u64, steps: Vec<SimStep>) -> Self {
            Self {
                id: ChainId(id),
                steps: Mutex::new(steps),
                call_count: Mutex::new(0),
                last_req: Mutex::new(None),
            }
        }
    }

    impl ExecutionEngine for Arc<FakeEngine> {
        fn chain_id(&self) -> ChainId {
            self.id
        }

        fn simulate(
            &self,
            request: SimulationRequest,
        ) -> Result<SimulationOutput, Box<dyn std::error::Error + Send + Sync>> {
            *self.last_req.lock().unwrap() = Some(request);
            let mut count = self.call_count.lock().unwrap();
            let steps = self.steps.lock().unwrap();
            if *count < steps.len() {
                let idx = *count;
                *count += 1;
                let step = &steps[idx];
                if let Some(ref e) = step.err {
                    return Err(e.to_string().into());
                }
                return Ok(SimulationOutput {
                    read_request: step.read.clone(),
                    write_messages: step.write.clone(),
                });
            }
            *count += 1;
            Ok(SimulationOutput {
                read_request: None,
                write_messages: Vec::new(),
            })
        }
    }

    #[derive(Debug, Default)]
    struct FakeSeqNetwork {
        votes: Mutex<Vec<bool>>,
        mailbox_sent: Mutex<Vec<(ChainId, MailboxMessage)>>,
    }

    impl SequencerNetwork for Arc<FakeSeqNetwork> {
        fn send_mailbox_message(&self, recipient: ChainId, msg: MailboxMessage) {
            self.mailbox_sent.lock().unwrap().push((recipient, msg));
        }
        fn send_vote(&self, vote: bool) {
            self.votes.lock().unwrap().push(vote);
        }
    }

    type TestSeqInst = SequencerInstance<Arc<FakeEngine>, Arc<FakeSeqNetwork>>;

    #[allow(clippy::needless_pass_by_value)]
    fn make_seq(
        chain_id: u64,
        steps: Vec<SimStep>,
        inst: Instance,
    ) -> (TestSeqInst, Arc<FakeEngine>, Arc<FakeSeqNetwork>) {
        let eng = Arc::new(FakeEngine::new(chain_id, steps));
        let net = Arc::new(FakeSeqNetwork::default());
        let seq = SequencerInstance::new(
            &inst,
            Arc::clone(&eng),
            Arc::clone(&net),
            StateRoot::default(),
        )
        .unwrap();
        (seq, eng, net)
    }

    fn simple_inst(chain_txs: &[(u64, Vec<&str>)]) -> Instance {
        Instance {
            id: InstanceId::default(),
            period_id: 0.into(),
            sequence_number: 0.into(),
            xt_request: XtRequest {
                transactions: chain_txs
                    .iter()
                    .map(|(chain, txs)| TransactionRequest {
                        chain_id: ChainId(*chain),
                        transactions: txs.iter().map(|t| t.as_bytes().to_vec()).collect(),
                    })
                    .collect(),
            },
        }
    }

    use compose_spec::InstanceId;

    #[test]
    fn vote_true_on_immediate_success() {
        let inst = simple_inst(&[(1, vec!["a"]), (2, vec!["b"])]);
        let (seq, _, net) = make_seq(1, vec![], inst);

        seq.run().unwrap();
        let votes = net.votes.lock().unwrap();
        assert_eq!(votes.len(), 1);
        assert!(votes[0]);
        drop(votes);

        assert_eq!(seq.decision_state(), DecisionState::Pending);
        assert_eq!(seq.state(), SequencerState::WaitingDecided);

        seq.process_decided_message(true);
        assert_eq!(seq.decision_state(), DecisionState::Accepted);
        assert_eq!(seq.state(), SequencerState::Done);

        // Subsequent decided is ignored
        seq.process_decided_message(false);
        assert_eq!(seq.decision_state(), DecisionState::Accepted);
    }

    #[test]
    fn read_then_mailbox_then_success() {
        let need = make_msg(2, "X", b"d1");
        let steps = vec![
            SimStep {
                read: Some(need.header.clone()),
                write: vec![],
                err: None,
            },
            SimStep {
                read: None,
                write: vec![],
                err: None,
            },
        ];
        let inst = simple_inst(&[(1, vec!["a"])]);
        let (seq, _, net) = make_seq(1, steps, inst);

        seq.run().unwrap();
        assert!(net.votes.lock().unwrap().is_empty());

        seq.process_mailbox_message(need).unwrap();
        let votes = net.votes.lock().unwrap();
        assert_eq!(votes.len(), 1);
        assert!(votes[0]);
    }

    #[test]
    fn multiple_reads_out_of_order() {
        let a = make_msg(2, "A", b"a");
        let b = make_msg(3, "B", b"b");
        let steps = vec![
            SimStep {
                read: Some(a.header.clone()),
                write: vec![],
                err: None,
            },
            SimStep {
                read: Some(b.header.clone()),
                write: vec![],
                err: None,
            },
            SimStep {
                read: None,
                write: vec![],
                err: None,
            },
        ];
        let inst = simple_inst(&[(1, vec!["x"])]);
        let (seq, _, net) = make_seq(1, steps, inst);

        seq.run().unwrap();

        // Deliver B first (out of order) — should not trigger until A arrives
        seq.process_mailbox_message(b).unwrap();
        assert!(net.votes.lock().unwrap().is_empty());

        // Deliver A; engine will ask for B, which is already buffered
        seq.process_mailbox_message(a).unwrap();
        let votes = net.votes.lock().unwrap();
        assert_eq!(votes.len(), 1);
        assert!(votes[0]);
    }

    #[test]
    fn timeout_before_vote_sends_false() {
        let need = make_msg(2, "NEED", &[]);
        let steps = vec![SimStep {
            read: Some(need.header.clone()),
            write: vec![],
            err: None,
        }];
        let inst = simple_inst(&[(1, vec!["x"])]);
        let (seq, _, net) = make_seq(1, steps, inst);

        seq.run().unwrap();
        assert_eq!(seq.decision_state(), DecisionState::Pending);
        assert_eq!(seq.state(), SequencerState::Simulating);

        seq.timeout();
        let votes = net.votes.lock().unwrap();
        assert_eq!(votes.len(), 1);
        assert!(!votes[0]);
        drop(votes);

        assert_eq!(seq.decision_state(), DecisionState::Rejected);
        assert_eq!(seq.state(), SequencerState::Done);

        // Now mailbox is ignored
        seq.process_mailbox_message(need).unwrap();
    }

    #[test]
    fn timeout_with_multiple_unfulfilled_reads() {
        let msg_a = MailboxMessage {
            header: MailboxMessageHeader {
                source_chain_id: ChainId(2),
                dest_chain_id: ChainId(1),
                sender: EthAddress([1; 20]),
                receiver: EthAddress([2; 20]),
                session_id: SessionId(100),
                label: "labelA".into(),
            },
            data: vec![],
        };
        let msg_b = MailboxMessage {
            header: MailboxMessageHeader {
                source_chain_id: ChainId(3),
                dest_chain_id: ChainId(1),
                sender: EthAddress([1; 20]),
                receiver: EthAddress([2; 20]),
                session_id: SessionId(100),
                label: "labelB".into(),
            },
            data: vec![],
        };

        let steps = vec![
            SimStep {
                read: Some(msg_a.header.clone()),
                write: vec![],
                err: None,
            },
            SimStep {
                read: Some(msg_b.header.clone()),
                write: vec![],
                err: None,
            },
        ];
        let inst = simple_inst(&[(1, vec!["tx"])]);
        let (seq, _, net) = make_seq(1, steps, inst);

        seq.run().unwrap();
        assert_eq!(seq.state(), SequencerState::Simulating);

        let expected = seq.expected_read_requests();
        assert_eq!(expected.len(), 1);
        assert_eq!(expected[0], msg_a.header);

        assert!(net.votes.lock().unwrap().is_empty());

        seq.timeout();
        assert_eq!(seq.state(), SequencerState::Done);
        assert_eq!(seq.decision_state(), DecisionState::Rejected);

        let votes = net.votes.lock().unwrap();
        assert_eq!(votes.len(), 1);
        assert!(!votes[0]);
        drop(votes);

        let expected = seq.expected_read_requests();
        assert_eq!(expected.len(), 1);
        assert_eq!(expected[0].source_chain_id, ChainId(2));
        assert_eq!(expected[0].dest_chain_id, ChainId(1));
        assert_eq!(expected[0].session_id, SessionId(100));
        assert_eq!(expected[0].label, "labelA");
    }

    #[test]
    fn timeout_after_waiting_decided_ignored() {
        let inst = simple_inst(&[(1, vec!["tx"])]);
        let (seq, _, net) = make_seq(1, vec![], inst);

        seq.run().unwrap();
        assert_eq!(seq.state(), SequencerState::WaitingDecided);
        let votes = net.votes.lock().unwrap();
        assert_eq!(votes.len(), 1);
        assert!(votes[0]);
        drop(votes);

        seq.timeout();
        assert_eq!(seq.state(), SequencerState::WaitingDecided);
        assert_eq!(seq.decision_state(), DecisionState::Pending);
        assert_eq!(net.votes.lock().unwrap().len(), 1);
    }

    #[test]
    fn simulation_error_votes_false() {
        let steps = vec![SimStep {
            read: None,
            write: vec![],
            err: Some("boom".into()),
        }];
        let inst = simple_inst(&[(1, vec!["x"])]);
        let (seq, _, net) = make_seq(1, steps, inst);

        let err = seq.run().unwrap_err();
        assert!(err.to_string().contains("simulating sequencer failed"));
        assert!(err.to_string().contains("boom"));

        let votes = net.votes.lock().unwrap();
        assert_eq!(votes.len(), 1);
        assert!(!votes[0]);
    }

    #[test]
    fn filters_transactions_by_chain_id() {
        let inst = simple_inst(&[(42, vec!["mine1", "mine2"]), (7, vec!["other"])]);
        let (seq, eng, _) = make_seq(42, vec![], inst);

        seq.run().unwrap();

        let last_req = eng.last_req.lock().unwrap();
        let req = last_req.as_ref().unwrap();
        let names: Vec<String> = req
            .transactions
            .iter()
            .map(|t| String::from_utf8(t.clone()).unwrap())
            .collect();
        assert_eq!(names, vec!["mine1", "mine2"]);
    }

    #[test]
    fn no_transactions_returns_error() {
        let eng = Arc::new(FakeEngine::new(42, vec![]));
        let net = Arc::new(FakeSeqNetwork::default());
        let inst = simple_inst(&[(7, vec!["other"])]);

        let result = SequencerInstance::new(
            &inst,
            Arc::clone(&eng),
            Arc::clone(&net),
            StateRoot::default(),
        );
        assert!(result.is_err());
        assert!(matches!(
            result.unwrap_err(),
            SequencerError::NoTransactions
        ));
        assert!(net.votes.lock().unwrap().is_empty());
    }
}
