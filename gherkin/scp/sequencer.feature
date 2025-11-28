Feature: Sequencer
  Once a StartInstance message is received, the sequencer filter its own transactions, sets a timer,
  and begins simulating with a mailbox tracer.
  It exchange messages with other sequencers and,
  once the simulation produces a result, it votes and waits for a decision.

  Background:
    Given there is a chain "1" with sequencer "A"
    Given there is a chain "2" with sequencer "B"

  Scenario: Transaction filtering and timer
    When sequencer "A" receives StartInstance with instance ID "0x1", period ID "1", sequence number "1", and xtrequest "1: [tx1_1,tx1_2], 2: [tx2]"
    Then sequencer "A" should filter the transactions "[tx1_1,tx1_2]"
    And sequencer "A" should start a timer for the instance "0x1"

  Scenario: No transaction raises error
    When sequencer "A" receives StartInstance with instance ID "0x1", period ID "1", sequence number "1", and xtrequest "2: [tx2], 3: [tx3]"
    Then error "No transactions for this chain" should be returned

  Scenario: Simulation raises error different from read miss
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    When the execution engine simulates "tx1" and returns an error different than "Read miss"
    Then sequencer "A" should send Vote to the shared publisher with instance ID "0x1", chain ID "1", and vote "false"

  Scenario: Simulation successful
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    When the execution engine simulates "tx1" and returns success
    Then sequencer "A" should send Vote to the shared publisher with instance ID "0x1", chain ID "1", and vote "true"

  Scenario: Read miss handled by storing expected message
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    When the execution engine simulates "tx1" and returns a read miss for message with source chain ID "2", destination chain ID "1", source "0xabc", receiver "0xdef", session ID "0x123", label "MSG", and data "data"
    Then sequencer "A" should store mailbox message with source chain ID "2", destination chain ID "1", source "0xabc", receiver "0xdef", session ID "0x123", label "MSG", and data "data" as expected message for instance ID "0x1"

  Scenario: Written message is sent to the destination sequencer
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "3", sequence number "9", and xtrequest "1: [tx1], 2: [tx2]"
    When the execution engine simulates "tx1" and emits a MailboxMessage with source chain "1", destination chain "2", source "0xaaa", receiver "0xbbb", session ID "0x777", label "TRANSFER", and data "[0x01,0x02]"
    Then sequencer "A" should send MailboxMessage to sequencer "B" with instance ID "0x1", source chain "1", destination chain "2", source "0xaaa", receiver "0xbbb", session ID "0x777", label "TRANSFER", and data "[0x01,0x02]"

  Scenario: A received mailbox message is stored as pending
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "3", sequence number "9", and xtrequest "1: [tx1], 2: [tx2]"
    When sequencer "A" receives MailboxMessage with instance ID "0x1", source chain "2", destination chain "1", source "0x222", receiver "0x111", session ID "0x999", label "NOTE", and data "[0xab]"
    Then sequencer "A" should append to the pending mailbox queue for instance ID "0x1" the message with source chain "2", destination chain "1", source "0x222", receiver "0x111", session ID "0x999", label "NOTE", and data "[0xab]"
    And sequencer "A" should not trigger a new simulation

  Scenario: A received mailbox message that is expected is added to the inbox and triggers a new simulation
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    And the execution engine simulates "tx1" and returns a read miss for message with source chain ID "2", destination chain ID "1", source "0xabc", receiver "0xdef", session ID "0x123", label "MSG", and data "data"
    And sequencer "A" should store mailbox message with source chain ID "2", destination chain ID "1", source "0xabc", receiver "0xdef", session ID "0x123", label "MSG", and data "data" as expected message for instance ID "0x1"
    When sequencer "A" receives MailboxMessage with instance ID "0x1", source chain "2", destination chain "1", source "0xabc", receiver "0xdef", session ID "0x123", label "MSG", and data "data"
    Then the sequencer should insert a mailbox.putInbox transaction above the transaction list for the message with source chain "2", destination chain "1", source "0xabc", receiver "0xdef", session ID "0x123", label "MSG", and data "data"
    And sequencer "A" should start a new simulation

  Scenario: Timeout before voting causes a rejection
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    When the timer for instance ID "0x1" expires
    Then sequencer "A" should send Vote to the shared publisher with instance ID "0x1", chain ID "1", and vote "false"
    And sequencer "A" should mark the instance as rejected

  Scenario Outline: Timeout after voting causes no action
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    And sequencer "A" sent Vote to the shared publisher with instance ID "0x1", chain ID "1", and vote "<vote>"
    When the timer for instance ID "0x1" expires
    Then no additional Vote message should be sent

    Examples:
      | vote  |
      | true  |
      | false |

  Scenario: Decision false causes rejection
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    When sequencer "A" receives Decided from the shared publisher with instance ID "0x1" and decision "false"
    Then sequencer "A" should mark the instance as rejected

  Scenario: If decision true is received but no vote was sent, an error is raised
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    And sequencer "A" has not sent Vote to the shared publisher with instance ID "0x1"
    When sequencer "A" receives Decided from the shared publisher with instance ID "0x1" and decision "true"
    Then error "decision true but no vote sent is an impossible state" should be returned

  Scenario: Decision true causes finalization
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    And the execution engine simulates "tx1" and returns success
    And sequencer "A" sent Vote to the shared publisher with instance ID "0x1", chain ID "1", and vote "true"
    When sequencer "A" receives Decided from the shared publisher with instance ID "0x1" and decision "true"
    Then sequencer "A" should accept the instance

  Scenario: Double decided is ignored and error is raised
    Given sequencer "A" receives StartInstance with instance ID "0x1", period ID "2", sequence number "2", and xtrequest "1: [tx1], 2: [tx2]"
    And sequencer "A" receives Decided from the shared publisher with instance ID "0x1" and decision "false"
    When sequencer "A" receives Decided from the shared publisher with instance ID "0x1" and decision "true"
    Then error "instance already decided" should be returned
