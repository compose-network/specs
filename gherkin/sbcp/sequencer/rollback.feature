Feature: Sequencer Rollback Handling
  When the SP cannot produce a settlement proof for a superblock, it broadcasts a Rollback message,
  resetting the network state to the last finalized superblock.
  Sequencers can only process the rollback if it matches their last finalized superblock.
  Upon acceptance, they must discard any pending block, clear active instances,
  delete sealed blocks that reference newer superblocks, and set their period and target
  superblock to the rollback parameters.

  Background:
    Given there is a chain "1" with sequencer "A"
    And the settled state for the sequencer "A" is:
      | field             | value  |
      | superblock_number | 4      |
      | superblock_hash   | 0x4001 |
      | block_number      | 100    |
      | block_hash        | 0xaa01 |

  @sequencer @sbcp @rollback
  Scenario: Rejects rollback that does not match the settled state
    When the sequencer "A" receives Rollback:
      | field             | value  |
      | superblock_number | 5      |
      | superblock_hash   | 0x5001 |
      | period_id         | 9      |
    Then it should fail with error:
      """
      mismatched finalized state
      """

  @sequencer @sbcp @rollback
  Scenario: Applies rollback and clears blocks beyond the finalized superblock
    Given the sequencer "A" sealed blocks "101,102" for period "8" targeting superblock "3"
    And the sequencer "A" sealed blocks "103,104" for period "9" targeting superblock "4"
    And the sequencer "A" sealed blocks "105,106" for period "10" targeting superblock "5"
    And the sequencer "A" has an open block "107" for period "11" targeting superblock "6"
    And the sequencer "A" has an active instance "0xdef"
    When the sequencer "A" receives Rollback from publisher:
      | field             | value  |
      | superblock_number | 4      |
      | superblock_hash   | 0x4001 |
      | period_id         | 12     |
    Then the sequencer "A" should accept the Rollback
    And the sequencer "A" should delete the open block "107", ending up with no open block
    And the sequencer "A" should clear the active instance "0xdef", ending up with no active instance
    And the sequencer "A" should have sealed blocks "101,102" for period "8" targeting superblock "3"
    And the sequencer "A" should have sealed blocks "103,104" for period "9" targeting superblock "4"
    And the sequencer "A" should have no sealed blocks targeting superblock greater than "4"
    And the sequencer "A" should have as head the block "104"
    And the sequencer "A" should have current period "12" and target superblock "5"
