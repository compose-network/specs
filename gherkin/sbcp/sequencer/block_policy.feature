Feature: Sequencer Block Policy
  A sequencer must build L2 blocks sequentially, tagging every block with the active period ID
  and target superblock number announced by the publisher.
  While a composability instance is active, local transactions cannot be executed.
  Blocks cannot be sealed while an instance is active and
  the sequencer must guarantee only one block is open at a time.

  Background:
    Given there is a chain "1" with sequencer "A"
    And the sequencer "A" is at period ID "10" targeting superblock "9"

  @sequencer @sbcp @blocks
  Scenario Outline: Starting a new block with a non-sequential number is rejected
    Given the sequencer "A" has no open block
    And the sequencer "A" last sealed block number is <last_sealed>
    When the sequencer "A" attempts to begin building block <new_block>
    Then the attempt should fail with error:
      """
      block number is not sequential
      """

    Examples:
      | last_sealed | new_block |
      | 100         | 100       |
      | 100         | 102       |
      | 100         | 103       |
      | 100         | 99        |

  @sequencer @sbcp @blocks
  Scenario Outline: Starting a new block with an already open block is rejected
    Given the sequencer "A" has an open block "101"
    When the sequencer "A" attempts to begin building block <new_block>
    Then the attempt should fail with error:
      """
      there is already an open block
      """
    Examples:
      | new_block |
      | 101       |
      | 102       |
      | 103       |

  @sequencer @sbcp @blocks
  Scenario Outline: Successful block beginning
    Given the sequencer "A" has no open block
    And the sequencer "A" last sealed block number is <last_sealed>
    When the sequencer "A" begins building block <new_block>
    Then the sequencer "A" should have a new open block <new_block> with period "10" and superblock "9"
    Examples:
      | last_sealed | new_block |
      | 100         | 101       |
      | 101         | 102       |
      | 102         | 103       |


  @sequencer @sbcp @blocks
  Scenario: Starting an instance locks local transactions from being processed
    Given the sequencer "A" has an open block "101"
    And the sequencer "A" has an active instance "0xabc"
    When the sequencer "A" attempts to add local transaction "0x1" to block "101"
    Then the attempt should fail with error:
      """
      local transactions are disabled while an instance is active
      """

  @sequencer @sbcp @blocks
  Scenario: Local transactions can be added if there is no active instance
    Given the sequencer "A" has an open block "101"
    And the sequencer "A" has no active instance
    When the sequencer "A" attempts to add local transaction "0x1" to block "101"
    Then the local transaction "0x1" should be added to block "101"


  @sequencer @sbcp @blocks
  Scenario: Blocks cannot be sealed while an instance is active
    Given the sequencer "A" has an open block "101"
    And the sequencer "A" has an active instance "0xdef"
    When the sequencer "A" attempts to seal block "101"
    Then the attempt should fail with error:
      """
      there is already an active instance
      """

  @sequencer @sbcp @blocks
  Scenario: Blocks can be sealed if there is no active instance
    Given the sequencer "A" has an open block "101"
    And the sequencer "A" has no active instance
    When the sequencer "A" attempts to seal block "101"
    Then the block "101" should be successfully sealed
