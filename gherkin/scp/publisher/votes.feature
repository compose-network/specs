Feature: Publisher Vote Processing
  The shared publisher collects votes from the sequencers that were included in the XTRequest.
  It keeps one vote per chain, rejects invalid senders, and finalizes the instance once the
  voting rules are satisfied.

  Background:
    Given there is a shared publisher "SP"
    And there is a chain "1" with sequencer "A"
    And there is a chain "2" with sequencer "B"
    And there is a chain "3" with sequencer "C"

  @publisher @scp @votes
  Scenario: Accepts instance after collecting all positive votes
    Given the shared publisher "SP" started an instance:
      """
      instance_id: 0x3
      period_id: 9
      sequence_number: 5
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    When sequencer "A" publishes Vote with:
      | field       | value |
      | instance_id | 0x3   |
      | chain_id    | 1     |
      | vote        | true  |
    And sequencer "B" publishes Vote with:
      | field       | value |
      | instance_id | 0x3   |
      | chain_id    | 2     |
      | vote        | true  |
    Then the shared publisher "SP" should publish Decided with:
      | field       | value |
      | instance_id | 0x3   |
      | decision    | true  |
    And the shared publisher "SP" should mark the instance "0x3" as accepted

  @publisher @scp @votes
  Scenario: Rejects instance immediately when a vote is false
    Given the shared publisher "SP" started an instance:
      """
      instance_id: 0x4
      period_id: 10
      sequence_number: 6
      xtrequest:
        1: [tx1]
        2: [tx2]
        3: [tx3]
      """
    When sequencer "C" publishes Vote with:
      | field       | value |
      | instance_id | 0x4   |
      | chain_id    | 3     |
      | vote        | false |
    Then the shared publisher "SP" should publish Decided with:
      | field       | value |
      | instance_id | 0x4   |
      | decision    | false |
    And the shared publisher "SP" should mark the instance "0x4" as rejected
    And subsequent votes for instance "0x4" should be ignored

  @publisher @scp @votes
  Scenario: Errors when receiving a duplicated vote from the same chain
    Given the shared publisher "SP" started an instance:
      """
      instance_id: 0x5
      period_id: 11
      sequence_number: 7
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And sequencer "A" has already published Vote with:
      | field       | value |
      | instance_id | 0x5   |
      | chain_id    | 1     |
      | vote        | true  |
    When sequencer "A" publishes another Vote for instance "0x5"
    Then an error occurs:
      """
      duplicated vote for chain 1
      """

  @publisher @scp @votes
  Scenario: Rejects votes from chains that are not part of the instance
    Given the shared publisher "SP" started an instance:
      """
      instance_id: 0x6
      period_id: 12
      sequence_number: 8
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    When sequencer "C" publishes Vote with:
      | field       | value |
      | instance_id | 0x6   |
      | chain_id    | 3     |
      | vote        | true  |
    Then an error occurs:
      """
      vote from non-participant chain
      """

  @publisher @scp @votes
  Scenario: Ignores votes that arrive after the instance has been decided
    Given the shared publisher "SP" started an instance:
      """
      instance_id: 0x7
      period_id: 13
      sequence_number: 9
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    And sequencer "A" has already published Vote with:
      | field       | value |
      | instance_id | 0x7   |
      | chain_id    | 1     |
      | vote        | false |
    And the shared publisher "SP" has already published Decided for instance "0x7" with decision "false"
    When sequencer "B" publishes Vote with:
      | field       | value |
      | instance_id | 0x7   |
      | chain_id    | 2     |
      | vote        | true  |
    Then no additional Decided message should be sent for instance "0x7"
