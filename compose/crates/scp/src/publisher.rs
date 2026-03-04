use std::{collections::HashMap, sync::Mutex};

use compose_spec::{ChainId, DecisionState, Instance, InstanceId};
use thiserror::Error;
use tracing::info;

#[derive(Debug, Error)]
pub enum PublisherError {
    #[error("duplicated vote")]
    DuplicatedVote,
    #[error("sender is not a participant")]
    SenderNotParticipant,
}

/// Network interface for the publisher side of an SCP instance.
pub trait PublisherNetwork: Send + Sync {
    fn send_start_instance(&self, instance: &Instance);
    fn send_decided(&self, instance_id: InstanceId, decided: bool);
}

struct PublisherInner {
    instance: Instance,
    chains: Vec<ChainId>,
    decision_state: DecisionState,
    votes: HashMap<ChainId, bool>,
}

/// Publisher-side logic for a single SCP instance (2PC coordinator).
pub struct PublisherInstance<N: PublisherNetwork> {
    inner: Mutex<PublisherInner>,
    network: N,
}

impl<N: PublisherNetwork> std::fmt::Debug for PublisherInstance<N> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("PublisherInstance").finish_non_exhaustive()
    }
}

impl<N: PublisherNetwork> PublisherInstance<N> {
    pub fn new(instance: Instance, network: N) -> Self {
        let chains = instance.chains();
        Self {
            inner: Mutex::new(PublisherInner {
                instance,
                chains,
                decision_state: DecisionState::Pending,
                votes: HashMap::new(),
            }),
            network,
        }
    }

    #[must_use]
    pub fn decision_state(&self) -> DecisionState {
        self.inner.lock().unwrap().decision_state
    }

    #[must_use]
    pub fn instance(&self) -> Instance {
        self.inner.lock().unwrap().instance.clone()
    }

    /// Launches the instance by broadcasting `StartInstance` to all participants.
    pub fn run(&self) {
        let inner = self.inner.lock().unwrap();
        self.network.send_start_instance(&inner.instance);
    }

    pub fn process_vote(&self, sender: ChainId, vote: bool) -> Result<(), PublisherError> {
        let mut inner = self.inner.lock().unwrap();

        if inner.decision_state != DecisionState::Pending {
            info!(
                chain_id = sender.get(),
                vote, "Ignoring vote because already decided"
            );
            return Ok(());
        }

        if inner.votes.contains_key(&sender) {
            info!(chain_id = sender.get(), vote, "Ignoring duplicated vote");
            return Err(PublisherError::DuplicatedVote);
        }

        if !inner.chains.contains(&sender) {
            info!(
                chain_id = sender.get(),
                vote, "Ignoring vote from non-participant"
            );
            return Err(PublisherError::SenderNotParticipant);
        }

        inner.votes.insert(sender, vote);

        // Any reject vote decides false immediately
        if !vote {
            info!(
                chain_id = sender.get(),
                "Received reject vote, rejecting instance"
            );
            inner.decision_state = DecisionState::Rejected;
            self.network.send_decided(inner.instance.id, false);
            return Ok(());
        }

        // Check if all votes are in
        if inner.votes.len() == inner.chains.len() {
            info!("All votes received, accepting instance");
            inner.decision_state = DecisionState::Accepted;
            self.network.send_decided(inner.instance.id, true);
        }

        Ok(())
    }

    pub fn timeout(&self) {
        let mut inner = self.inner.lock().unwrap();

        if inner.decision_state != DecisionState::Pending {
            info!("Ignoring timeout because already decided");
            return;
        }

        info!("Instance timed out, rejecting");
        inner.decision_state = DecisionState::Rejected;
        self.network.send_decided(inner.instance.id, false);
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use compose_spec::{TransactionRequest, XtRequest};

    use super::*;

    fn tx_req(chain: u64, payloads: &[&str]) -> TransactionRequest {
        TransactionRequest {
            chain_id: ChainId(chain),
            transactions: payloads.iter().map(|p| p.as_bytes().to_vec()).collect(),
        }
    }

    #[derive(Debug, Default)]
    struct FakeNetwork {
        start_called: Mutex<u32>,
        decided_called: Mutex<u32>,
        decisions: Mutex<Vec<(InstanceId, bool)>>,
    }

    impl FakeNetwork {
        fn start_count(&self) -> u32 {
            *self.start_called.lock().unwrap()
        }
        fn decided_count(&self) -> u32 {
            *self.decided_called.lock().unwrap()
        }
        fn decisions(&self) -> Vec<(InstanceId, bool)> {
            self.decisions.lock().unwrap().clone()
        }
    }

    impl PublisherNetwork for Arc<FakeNetwork> {
        fn send_start_instance(&self, _instance: &Instance) {
            *self.start_called.lock().unwrap() += 1;
        }
        fn send_decided(&self, instance_id: InstanceId, decided: bool) {
            *self.decided_called.lock().unwrap() += 1;
            self.decisions.lock().unwrap().push((instance_id, decided));
        }
    }

    fn make_pub(inst: Instance) -> (PublisherInstance<Arc<FakeNetwork>>, Arc<FakeNetwork>) {
        let net = Arc::new(FakeNetwork::default());
        let pub_inst = PublisherInstance::new(inst, Arc::clone(&net));
        (pub_inst, net)
    }

    #[test]
    fn all_true_votes_decides_true() {
        let inst = Instance {
            id: InstanceId([1; 32]),
            period_id: 7.into(),
            sequence_number: 3.into(),
            xt_request: XtRequest {
                transactions: vec![tx_req(1, &["a"]), tx_req(2, &["b"])],
            },
        };

        let (pub_inst, net) = make_pub(inst.clone());
        assert_eq!(pub_inst.instance(), inst);
        assert_eq!(pub_inst.decision_state(), DecisionState::Pending);

        assert_eq!(net.start_count(), 0, "unexpected start before Run");
        pub_inst.run();
        assert_eq!(net.start_count(), 1);
        assert_eq!(pub_inst.decision_state(), DecisionState::Pending);

        pub_inst.process_vote(ChainId(1), true).unwrap();
        assert_eq!(net.decided_count(), 0, "should not decide yet");
        assert_eq!(pub_inst.decision_state(), DecisionState::Pending);

        // Duplicate should error
        let err = pub_inst.process_vote(ChainId(1), true).unwrap_err();
        assert!(matches!(err, PublisherError::DuplicatedVote));
        assert_eq!(pub_inst.decision_state(), DecisionState::Pending);

        // Second true triggers accept
        pub_inst.process_vote(ChainId(2), true).unwrap();
        assert_eq!(net.decided_count(), 1);
        assert_eq!(pub_inst.decision_state(), DecisionState::Accepted);
        let decisions = net.decisions();
        assert_eq!(decisions.len(), 1);
        assert!(decisions[0].1);
        assert_eq!(decisions[0].0, inst.id);

        // Duplicate after decision is ignored
        pub_inst.process_vote(ChainId(1), true).unwrap();

        // Vote after done is ignored
        pub_inst.process_vote(ChainId(3), true).unwrap();
        assert_eq!(net.decided_count(), 1, "unexpected extra decided calls");
        assert_eq!(pub_inst.decision_state(), DecisionState::Accepted);
    }

    #[test]
    fn non_participant_vote_errors() {
        let inst = Instance {
            id: InstanceId([2; 32]),
            period_id: 0.into(),
            sequence_number: 0.into(),
            xt_request: XtRequest {
                transactions: vec![tx_req(1, &["a"]), tx_req(2, &["b"])],
            },
        };

        let (pub_inst, net) = make_pub(inst);
        pub_inst.run();

        let err = pub_inst.process_vote(ChainId(99), true).unwrap_err();
        assert!(matches!(err, PublisherError::SenderNotParticipant));
        assert_eq!(net.decided_count(), 0);
        assert_eq!(pub_inst.decision_state(), DecisionState::Pending);

        pub_inst.process_vote(ChainId(1), true).unwrap();
        pub_inst.process_vote(ChainId(2), true).unwrap();
        assert_eq!(net.decided_count(), 1);
        assert_eq!(pub_inst.decision_state(), DecisionState::Accepted);
    }

    #[test]
    fn any_false_decides_false_early() {
        let inst = Instance {
            id: InstanceId::default(),
            period_id: 0.into(),
            sequence_number: 0.into(),
            xt_request: XtRequest {
                transactions: vec![tx_req(10, &["a"]), tx_req(11, &["b"]), tx_req(12, &["c"])],
            },
        };

        let (pub_inst, net) = make_pub(inst.clone());
        pub_inst.run();
        assert_eq!(pub_inst.decision_state(), DecisionState::Pending);

        pub_inst.process_vote(ChainId(11), false).unwrap();
        assert_eq!(net.decided_count(), 1);
        assert_eq!(pub_inst.decision_state(), DecisionState::Rejected);
        let decisions = net.decisions();
        assert!(!decisions[0].1);
        assert_eq!(decisions[0].0, inst.id);

        // Further votes are ignored
        pub_inst.process_vote(ChainId(12), true).unwrap();
        assert_eq!(net.decided_count(), 1, "unexpected extra decided calls");
        assert_eq!(pub_inst.decision_state(), DecisionState::Rejected);
    }

    #[test]
    fn timeout_decides_false() {
        let inst = Instance {
            id: InstanceId::default(),
            period_id: 0.into(),
            sequence_number: 0.into(),
            xt_request: XtRequest {
                transactions: vec![tx_req(5, &["a"]), tx_req(6, &["b"])],
            },
        };

        let (pub_inst, net) = make_pub(inst.clone());
        pub_inst.run();
        assert_eq!(pub_inst.decision_state(), DecisionState::Pending);

        pub_inst.timeout();
        assert_eq!(net.decided_count(), 1);
        assert_eq!(pub_inst.decision_state(), DecisionState::Rejected);
        let decisions = net.decisions();
        assert!(!decisions[0].1);
        assert_eq!(decisions[0].0, inst.id);

        // Timeout after done is ignored
        pub_inst.timeout();
        assert_eq!(net.decided_count(), 1);
    }

    #[test]
    fn timeout_after_true_decision_ignored() {
        let inst = Instance {
            id: InstanceId::default(),
            period_id: 0.into(),
            sequence_number: 0.into(),
            xt_request: XtRequest {
                transactions: vec![tx_req(1, &["a"]), tx_req(2, &["b"])],
            },
        };

        let (pub_inst, net) = make_pub(inst);
        pub_inst.run();

        pub_inst.process_vote(ChainId(1), true).unwrap();
        pub_inst.process_vote(ChainId(2), true).unwrap();
        assert_eq!(net.decided_count(), 1);
        assert_eq!(pub_inst.decision_state(), DecisionState::Accepted);

        pub_inst.timeout();
        assert_eq!(net.decided_count(), 1);
        assert_eq!(pub_inst.decision_state(), DecisionState::Accepted);
    }

    #[test]
    fn one_vote_then_timeout_decides_false() {
        let inst = Instance {
            id: InstanceId::default(),
            period_id: 0.into(),
            sequence_number: 0.into(),
            xt_request: XtRequest {
                transactions: vec![tx_req(1, &["a"]), tx_req(2, &["b"])],
            },
        };

        let (pub_inst, net) = make_pub(inst);
        pub_inst.run();

        pub_inst.process_vote(ChainId(1), true).unwrap();
        assert_eq!(pub_inst.decision_state(), DecisionState::Pending);

        pub_inst.timeout();
        assert_eq!(net.decided_count(), 1);
        assert_eq!(pub_inst.decision_state(), DecisionState::Rejected);
        let decisions = net.decisions();
        assert!(!decisions[0].1);
    }
}
