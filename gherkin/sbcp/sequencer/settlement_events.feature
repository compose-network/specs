Feature: Sequencer L1 Settlement Events
  Sequencers listen to L1 events about finalized superblocks and update their settled state accordingly.

  Background:
    Given there is a chain "1" with sequencer "A"

  @sequencer @sbcp @l1_events
  Scenario: Advances settled state monotonically using L1 events
    Given the sequencer "A" has the current settled superblock:
      | field             | value |
      | superblock_number | 5     |
      | superblock_hash   | 0xaaa |
    When an L1 event is received for:
        | field             | value |
        | superblock_number | 6     |
        | superblock_hash   | 0xbbbb |
    Then the sequencer "A" settled state should be superblock "6" with hash "0xbbbb"

  @sequencer @sbcp @l1_events
  Scenario: An L1 event with an old superblock is ignored
    Given the sequencer "A" has the current settled superblock:
      | field             | value |
      | superblock_number | 5     |
      | superblock_hash   | 0xaaa |
    When an L1 event is received for:
        | field             | value |
        | superblock_number | 4     |
        | superblock_hash   | 0xdddd |
    Then the sequencer "A" should ignore the event
    And the sequencer "A" settled state should remain superblock "5" with hash "0xaaa"
