Feature: Publisher Period Management
  The SP is responsible for managing time into periods, defining the target
  superblock number to be built, and notifying all sequencers.
  Each StartPeriod happy flow call increments both the period ID and the target superblock number,
  closing the previous period.
  Once a period ends, sequencers will produce zk proofs and submit them to the publisher,
  who will aggregate them via a superblock proof and publish to L1.
  For liveness, there is a proof window constraint that limits how many periods
  can elapse without a superblock proof being published to L1.
  In case a StartPeriod is called but a proof window exceedance is detected,
  the publisher broadcasts a Rollback, resetting the network state to the last finalized superblock,
  and clearing all ongoing activities.
  The proof window is exceeded when:
        new_target_superblock > (last_finalized_superblock_number + 1) + proof_window

  Background:
    Given the participating chains are "1,2,3,4"

  @publisher @sbcp @periods
  Scenario: Broadcasts the next StartPeriod
    Given SP is currently at period ID "10" targeting superblock "6"
    And the last finalized superblock is:
      | field        | value |
      | number       | 5     |
      | hash         | 0xabc |
    When SP starts a new period
    Then SP should broadcast StartPeriod with:
      | field               | value |
      | period_id           | 11     |
      | target_superblock   | 7     |

  @publisher @sbcp @periods @rollback
  Scenario: Rejects StartPeriod when the proof window would be exceeded
    Given SP is currently at period ID "30" targeting superblock <current_superblock>
    And SP enforces a proof window of <proof_window> periods
    And the last finalized superblock number is <last_finalized_superblock> with hash "0xabc"
    When SP attempts to start a new period
    Then the call should raise the error:
      """
      prof window exceeded
      """
    And no StartPeriod for period "31" should be emitted
    And SP should broadcast a Rollback with:
      | field        | value |
      | period_id    | 31    |
      | superblock   | <last_finalized_superblock>    |
      | hash         | 0xabc |
    And all active instances should be cleared
    And the next target superblock should reset to <superblock_reset>
    Examples:
      | proof_window | current_superblock | last_finalized_superblock | superblock_reset |
      | 1            | 12              | 10                           | 11               |
      | 2            | 13              | 10                           | 11               |
      | 10           | 21              | 10                           | 11               |
      | 10           | 20              | 9                            | 10               |

