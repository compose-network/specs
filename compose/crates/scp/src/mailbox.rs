use compose_spec::{ChainId, EthAddress, SessionId};

/// Header identifying a mailbox message exchanged between sequencers.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MailboxMessageHeader {
    pub session_id: SessionId,
    pub source_chain_id: ChainId,
    pub dest_chain_id: ChainId,
    pub sender: EthAddress,
    pub receiver: EthAddress,
    pub label: String,
}

/// Carries data exchanged between sequencers for mailbox fulfillment.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MailboxMessage {
    pub header: MailboxMessageHeader,
    pub data: Vec<u8>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn header_equality_ignores_data() {
        let a = MailboxMessage {
            header: MailboxMessageHeader {
                source_chain_id: ChainId(1),
                dest_chain_id: ChainId(2),
                sender: EthAddress([1; 20]),
                receiver: EthAddress([2; 20]),
                session_id: SessionId(10),
                label: "L".into(),
            },
            data: b"payload-A".to_vec(),
        };
        let b = MailboxMessage {
            header: a.header.clone(),
            data: b"payload-B".to_vec(),
        };

        assert_ne!(a.data, b.data, "Data should be different for this test");
        assert_eq!(
            a.header, b.header,
            "Data must be ignored for header equality"
        );

        // Flip each field and assert inequality
        let mut h = a.header.clone();
        h.source_chain_id = ChainId(99);
        assert_ne!(a.header, h);

        let mut h = a.header.clone();
        h.dest_chain_id = ChainId(99);
        assert_ne!(a.header, h);

        let mut h = a.header.clone();
        h.sender = EthAddress([9; 20]);
        assert_ne!(a.header, h);

        let mut h = a.header.clone();
        h.receiver = EthAddress([9; 20]);
        assert_ne!(a.header, h);

        let mut h = a.header.clone();
        h.session_id = SessionId(999);
        assert_ne!(a.header, h);

        let mut h = a.header.clone();
        h.label = "other".into();
        assert_ne!(a.header, h);
    }
}
