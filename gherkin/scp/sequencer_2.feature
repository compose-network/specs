Feature: Sequencer
  Once the sequencer receives a StartInstance message it filters its own transactions, sets a timer,
  and begins simulating with a mailbox tracer. It exchanges messages with peer sequencers and,
  after the simulation completes, it votes and waits for a decision.

  Background:
    Given there is a chain "1" with sequencer "A"
    And there is a chain "2" with sequencer "B"

  @start-instance
  Scenario: Filters local transactions and starts a timer
    When sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 1
      sequence_number: 1
      xtrequest:
        1: [tx1_1, tx1_2]
        2: [tx2]
      """
    Then sequencer "A" should only consider transactions "[tx1_1,tx1_2]" for simulation
    And a timer for instance "0x1" should start

  @start-instance
  Scenario: Rejects StartInstance with no local transactions
    When sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 1
      sequence_number: 1
      xtrequest:
        2: [tx2]
        3: [tx3]
      """
    Then an error occurs:
      """
      No transactions for this chain
      """

  @simulation
  Scenario Outline: Votes according to simulation outcome
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    When the execution engine simulates "tx1" and <result>
    Then sequencer "A" should publish Vote with:
      | field       | value  |
      | instance_id | 0x1    |
      | chain_id    | 1      |
      | vote        | <vote> |

    Examples:
      | result                                  | vote  |
      | returns success                         | true  |
      | returns an error other than "Read miss" | false |

  @mailbox @simulation
  Scenario: Records expected mailbox message after read miss
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    When the execution engine simulates "tx1" and returns a read miss for mailbox message:
      | field                | value |
      | source_chain_id      | 2     |
      | destination_chain_id | 1     |
      | source               | 0xabc |
      | receiver             | 0xdef |
      | session_id           | 0x123 |
      | label                | MSG   |
      | data                 | data  |
    Then sequencer "A" should record that mailbox message as expected for instance "0x1"

  @mailbox
  Scenario: Sends written mailbox message to the destination sequencer
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 3
      sequence_number: 9
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    When the execution engine simulates "tx1" and emits MailboxMessage:
      | field             | value      |
      | source_chain      | 1          |
      | destination_chain | 2          |
      | source            | 0xaaa      |
      | receiver          | 0xbbb      |
      | session_id        | 0x777      |
      | label             | TRANSFER   |
      | data              | [0x01,0x02] |
    Then sequencer "A" should forward that MailboxMessage to sequencer "B" with instance ID "0x1"

  @mailbox
  Scenario Outline: Handles inbound mailbox messages based on expectation
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And sequencer "A" <expected_state> an expected mailbox message with:
      | field             | value |
      | source_chain      | 2     |
      | destination_chain | 1     |
      | source            | 0xabc |
      | receiver          | 0xdef |
      | session_id        | 0x123 |
      | label             | MSG   |
      | data              | data  |
    When sequencer "A" receives MailboxMessage with the same payload and instance ID "0x1"
    Then <storage_result>
    And <simulation_effect>

    Examples:
      | expected_state   | storage_result                                                                                  | simulation_effect                                |
      | has not stored   | the message is appended to the pending mailbox queue for instance "0x1"                         | sequencer "A" should not start a new simulation  |
      | has already stored | the message is moved to the inbox and a mailbox.putInbox transaction is inserted before others | sequencer "A" should start a new simulation      |

  @timeout
  Scenario: Rejects instance when timer expires before voting
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    When the timer for instance ID "0x1" expires
    Then sequencer "A" should publish Vote with:
      | field       | value |
      | instance_id | 0x1   |
      | chain_id    | 1     |
      | vote        | false |
    And sequencer "A" should mark the instance "0x1" as rejected

  @timeout
  Scenario Outline: Ignores timer expiry after voting
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And sequencer "A" previously published Vote with:
      | field       | value  |
      | instance_id | 0x1    |
      | chain_id    | 1      |
      | vote        | <vote> |
    When the timer for instance ID "0x1" expires
    Then no additional Vote message should be sent

    Examples:
      | vote  |
      | true  |
      | false |

  @decision
  Scenario: Rejects instance when decision is false
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    When sequencer "A" receives Decided for instance "0x1" with decision "false"
    Then sequencer "A" should mark the instance "0x1" as rejected

  @decision
  Scenario: Errors when decision true arrives without a prior vote
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And sequencer "A" has not published a Vote for instance "0x1"
    When sequencer "A" receives Decided for instance "0x1" with decision "true"
    Then an error occurs:
      """
      decision true but no vote sent is an impossible state
      """

  @decision
  Scenario: Finalizes instance when decision true matches prior vote
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And the execution engine simulates "tx1" and returns success
    And sequencer "A" previously published Vote with:
      | field       | value |
      | instance_id | 0x1   |
      | chain_id    | 1     |
      | vote        | true  |
    When sequencer "A" receives Decided for instance "0x1" with decision "true"
    Then sequencer "A" should accept the instance "0x1"

  @decision
  Scenario: Raises error when a decided instance receives a second decision
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And sequencer "A" receives Decided for instance "0x1" with decision "false"
    When sequencer "A" later receives Decided for instance "0x1" with decision "true"
    Then an error occurs:
      """
      instance already decided
      """
