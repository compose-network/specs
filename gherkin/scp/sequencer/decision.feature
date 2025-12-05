Feature: Sequencer Decision
  On the sequencer perspective, an instance terminates in two cases:
  - when it sends a rejection vote (either due simulation failure or timeout)
  - when it receives a decision from the SP
  If the instance is decided as rejected, the sequencer should not include its transactions in a block.
  Else, it should include the transactions along with the created mailbox.putInbox ones.
  Note that the shared publisher can only send a decision true if all sequencers have voted true.
  Thus, receiving a decision true without having voted true is an invalid protocol state.

  Background:
    Given there is a chain "1" with sequencer "A"
    And there is a chain "2" with sequencer "B"

  @sequencer @scp @decision
  Scenario: Rejects instance upon simulation failure
    Given sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 2
      sequence_number: 2
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And the execution engine simulates "tx1" and returns an error other than "Read miss"
    When sequencer "A" publishes Vote with:
      | field       | value |
      | instance_id | 0x1   |
      | chain_id    | 1     |
      | vote        | false |
    Then sequencer "A" should mark the instance "0x1" as rejected

  @sequencer @scp @decision
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

  @sequencer @scp @decision
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

  @sequencer @scp @decision
  Scenario: Errors when decision true contradicts a prior false vote
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
      | field       | value |
      | instance_id | 0x1   |
      | chain_id    | 1     |
      | vote        | false |
    When sequencer "A" receives Decided for instance "0x1" with decision "true"
    Then an error occurs:
      """
      decision true but previous vote was false is an impossible state
      """

  @sequencer @scp @decision
  Scenario: Finalizes instance when decision is received from SP
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
    When sequencer "A" receives Decided for instance "0x1" with decision <decision>
    Then sequencer "A" should mark the instance "0x1" as <outcome>
    Examples:
      | decision | outcome   |
      | true     | accepted  |
      | false    | rejected  |

  @sequencer @scp @decision
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
