Feature: Sequencer Instance Management
  Sequencers must stop processing local transactions once a composability instance starts.
  An instance can only be started by the sequencer if it has a current open block.
  The new instance's period ID must match the period ID tagged on the open block.
  Furthermore, sequence numbers for instances must increase strictly within the same period.
  Only one active instance is allowed per sequencer at any time.
  Once the current instance gets decided, local transactions can be processed again and new instances are allowed.
  Users can submit an XTRequest to the sequencer, but the sequencer should only forward it to the SP and discard it.
  That is, only the SP is allowed to start new instances.

  Background:
    Given there is a chain "1" with sequencer "A"
    And the sequencer "A" is at period ID "20" targeting superblock "11"

  @sequencer @sbcp @instances
  Scenario: No open block rejects StartInstance requests
    Given the sequencer "A" has no open block
    When the sequencer "A" receives StartInstance for:
      | field           | value |
      | instance_id     | 0x1   |
      | period_id       | 20    |
      | sequence_number | 1     |
    Then the sequencer "A" should reject the instance by sending a vote "false" to the SP

  @sequencer @sbcp @instances
  Scenario Outline: Period id mismatch for instance and open block results in rejection
    Given the sequencer "A" has an open block tagged with period <block_period>
    When the sequencer "A" receives StartInstance for:
      | field           | value |
      | instance_id     | 0x1   |
      | period_id       | <instance_period>    |
      | sequence_number | 1     |
    Then the sequencer "A" should reject the instance by sending a vote "false" to the SP

    Examples:
      | block_period | instance_period |
      | 20           | 19              |
      | 20           | 21              |


  @sequencer @sbcp @instances
  Scenario Outline: Old or repeated sequence numbers result in rejection
    Given the sequencer "A" has an open block tagged with period "20"
    And the last accepted sequence number for period "20" is "<last_sequence>"
    When the sequencer "A" receives StartInstance for:
      | field           | value |
      | instance_id     | 0x1   |
      | period_id       | 20    |
      | sequence_number | <incoming_sequence> |
    Then the sequencer "A" should reject the instance by sending a vote "false" to the SP

    Examples:
      | last_sequence | incoming_sequence |
      | 3             | 2                 |
      | 5             | 5                 |

  @sequencer @sbcp @instances
  Scenario: Ongoing instance makes the sequencer reject new StartInstance requests
    Given the sequencer "A" has an open block tagged with period "20"
    And the last accepted sequence number for period "20" is "3"
    And the sequencer "A" has an active instance
    When the sequencer "A" receives StartInstance for:
      | field           | value |
      | instance_id     | 0x2   |
      | period_id       | 20    |
      | sequence_number | 4     |
    Then the sequencer "A" should reject the instance by sending a vote "false" to the SP

  @sequencer @sbcp @instances
  Scenario: Successful StartInstance locks local transactions
    Given the sequencer "A" has an open block tagged with period "20"
    And the sequencer "A" has no active instance
    When the sequencer "A" receives StartInstance for:
      | field           | value |
      | instance_id     | 0x1   |
      | period_id       | 20    |
      | sequence_number | 4     |
    Then the sequencer "A" should start the instance, register a snapshot of its state root, and lock local transactions


  @sequencer @sbcp @instances
  Scenario: Decided instance unlocks local transactions
    Given the sequencer "A" has an open block tagged with period "20"
    And the sequencer "A" has an active instance "0x1"
    When the sequencer "A" decides instance "0x1"
    Then the sequencer "A" should unlock and process local transactions
    And the sequencer "A" should have no active instance

  @sequencer @sbcp @instances
  Scenario: Decided event for a different instance is rejected
    Given the sequencer "A" has an open block tagged with period "20"
    And the sequencer "A" has an active instance "0x1"
    When the sequencer "A" decides instance "0x2"
    Then the attempt should fail with error:
      """
      mismatched active instance ID
      """

  @sequencer @sbcp @user-requests
  Scenario: Forwards user XTRequests to the SP
    When a user submits an XTRequest to sequencer "A":
      """
      1: [tx1]
      2: [tx2]
      """
    Then the sequencer "A" should forward to the SP the XTRequest:
        """
        1: [tx1]
        2: [tx2]
        """
