Feature: Sequencer Simulation
  After the startup, the sequencer should immediately start the simulation process.
  The simulation should return a success or an error, as well as the written mailbox messages.
  Any new written mailbox message should be forwarded to the destination sequencer.
  In case the result is a success, the sequencer should vote true.
  In case the result is an error other than "Read miss", the sequencer should vote false.
  In case of a "Read miss" error, the sequencer should record such message header as expected and wait for it.
  Once a mailbox message from another sequencer is received via the network, the sequencer should add it to the list
  of pending messages.
  Once the header of a new pending message matches the header of an expected one,
  the sequencer should create a mailbox.putInbox transaction for it and insert it before the other transactions,
  and then restart the simulation.

  Background:
    Given there is a chain "1" with sequencer "A"
    And there is a chain "2" with sequencer "B"

  @sequencer @scp @simulation
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

  @sequencer @scp @simulation @mailbox
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
    When the execution engine simulates "tx1" and returns a read miss for the mailbox message header:
      | field                | value |
      | source_chain_id      | 2     |
      | destination_chain_id | 1     |
      | source               | 0xabc |
      | receiver             | 0xdef |
      | session_id           | 0x123 |
      | label                | MSG   |
    Then sequencer "A" should record that mailbox message as expected for instance "0x1"

  @sequencer @scp @simulation @mailbox
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
    When the execution engine simulates "tx1" and writes a mailbox message with:
      | field             | value      |
      | source_chain      | 1          |
      | destination_chain | 2          |
      | source            | 0xaaa      |
      | receiver          | 0xbbb      |
      | session_id        | 0x777      |
      | label             | TRANSFER   |
      | data              | [0x01,0x02] |
    Then sequencer "A" should forward that MailboxMessage to sequencer "B" with instance ID "0x1"

  @sequencer @scp @simulation @mailbox
  Scenario: Sends multiple written mailbox messages to the destination sequencer
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 3
      sequence_number: 9
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    When the execution engine simulates "tx1" and writes the following mailbox messages:
      | source_chain | destination_chain | source | receiver | session_id | label     | data        |
      | 1            | 2                 | 0xaaa  | 0xbbb    | 0x777      | TRANSFER  | [0x01,0x02] |
      | 1            | 2                 | 0xccc  | 0xddd    | 0x888      | NOTE      | [0x03]      |
    Then sequencer "A" should forward the mailbox messages to sequencer "B" with instance ID "0x1"

  @sequencer @scp @simulation @mailbox
  Scenario: Does not resend a mailbox message that was already forwarded
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 3
      sequence_number: 9
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And sequencer "A" has already forwarded a mailbox message with:
      | field             | value      |
      | source_chain      | 1          |
      | destination_chain | 2          |
      | source            | 0xaaa      |
      | receiver          | 0xbbb      |
      | session_id        | 0x777      |
      | label             | TRANSFER   |
      | data              | [0x01,0x02] |
      | instance_id       | 0x1        |
    When the execution engine simulates "tx1" and writes the same mailbox message payload again
    Then no additional MailboxMessage should be forwarded

  @sequencer @scp @simulation @mailbox
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
    And sequencer "A" <expected_state> an expected mailbox message header with:
      | field             | value |
      | source_chain      | 2     |
      | destination_chain | 1     |
      | source            | 0xabc |
      | receiver          | 0xdef |
      | session_id        | 0x123 |
      | label             | MSG   |
    When sequencer "A" receives MailboxMessage with the same header and instance ID "0x1"
    Then <storage_result>
    And <simulation_effect>

    Examples:
      | expected_state     | storage_result                                                                                                 | simulation_effect                               |
      | has not stored     | the message is appended to the pending mailbox queue for instance "0x1"                                      | sequencer "A" should not start a new simulation |
      | has already stored | the message is removed from the expected set and pending queue, then inserted into the inbox and a mailbox.putInbox transaction is added | sequencer "A" should start a new simulation     |
