Feature: Sequencer Timeout
  If the timeout is reached, the sequencer should:
  - vote false in case it hasn't voted yet, marking the instance as rejected
  - ignore the timeout in case it has already voted

  Background:
    Given there is a chain "1" with sequencer "A"
    And there is a chain "2" with sequencer "B"

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
