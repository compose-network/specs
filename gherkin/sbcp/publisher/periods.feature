Feature: Publisher Period Management
  The SBCP publisher is responsible for managing time into periods, defining the target
  superblock number to be built, and notifying all sequencers.
  Each StartPeriod call increments both the period ID and the target superblock number,
  resets the per-period sequencing counter (for SCP instances), and must respect the configured proof window.
  When the proof window for a previous period elapses (no superblock proof was submitted to l1),
  the publisher must roll back every ongoing activity to the last finalized superblock.

  Background:
    Given there is a shared publisher "SP"
    And the participating chains are "1,2,3,4"

  @publisher @sbcp @periods
  Scenario: Broadcasts the next StartPeriod and resets the sequence counter
    Given the publisher "SP" is currently at period ID "10" targeting superblock "6"
    And the last finalized superblock is:
      | field        | value |
      | number       | 5     |
      | hash         | 0xabc |
    And the per-period sequence counter is "3"
    When the publisher "SP" starts a new period
    Then the publisher should broadcast StartPeriod with:
      | field               | value |
      | period_id           | 11     |
      | target_superblock   | 7     |
    And the per-period sequence counter should reset to "0"

  @publisher @sbcp @periods @rollback
  Scenario: Rejects StartPeriod when the proof window would be exceeded
    Given the publisher "SP" is currently at period ID "30" targeting superblock <current_superblock>
    And the publisher "SP" enforces a proof window of <proof_window> periods
    And the last finalized superblock number is "10" with hash "0xabc"
    When the publisher "SP" attempts to start a new period
    Then the call should raise the error:
      """
      prof window exceeded
      """
    And no StartPeriod broadcast should be emitted
    And the publisher should broadcast a Rollback with:
      | field        | value |
      | period_id    | 31    |
      | superblock   | 10    |
      | hash         | 0xabc |
    And all active instances should be cleared
    And the next target superblock should reset to "11"
    Examples:
      | proof_window | current_superblock |
      | 1            | 12              |
      | 2            | 13              |
      | 10           | 21              |

