Feature: Publisher Timeout
  The shared publisher starts a local timer when it launches an SCP instance.
  If the timer expires before a decision is sent, it must reject the instance and notify
  every participant. Once a decision is broadcast, any subsequent timeout should be ignored.

  Background:
    Given there is a shared publisher "SP"
    And there is a chain "1" with sequencer "A"
    And there is a chain "2" with sequencer "B"

  @publisher @scp @timeout
  Scenario: Rejects instance when the timer expires before all votes are received
    Given the shared publisher "SP" started an instance:
      """
      instance_id: 0x8
      period_id: 14
      sequence_number: 10
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And sequencer "A" publishes Vote with:
      | field       | value |
      | instance_id | 0x8   |
      | chain_id    | 1     |
      | vote        | true  |
    When the timer for the shared publisher "SP" expires for instance "0x8"
    Then the shared publisher "SP" should publish Decided with:
      | field       | value |
      | instance_id | 0x8   |
      | decision    | false |
    And the shared publisher "SP" should mark the instance "0x8" as rejected

  @publisher @scp @timeout
  Scenario Outline: Ignores timer expiry after a decision has been published
    Given the shared publisher "SP" started an instance:
      """
      instance_id: 0x9
      period_id: 15
      sequence_number: 11
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And the shared publisher "SP" has already published Decided for instance "0x9" with decision "<decision>"
    When the timer for the shared publisher "SP" expires for instance "0x9"
    Then no additional Decided message should be sent for instance "0x9"

    Examples:
      | decision |
      | true     |
      | false    |
