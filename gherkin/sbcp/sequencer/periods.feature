Feature: Sequencer Period and Settlement Management
  Each sequencer tracks the active period ID and the target superblock number announced by the publisher.
  When a new period starts, the sequencer updates those counters and immediately starts the settlement
  pipeline for the previous period if no block is pending. Otherwise, it waits until the pending block is sealed
  before requesting proofs and forwarding them to the publisher.
  Thus, the general rule is that: once a the last block for a previous period is sealed, the settlement pipeline
  for that period is triggered.

  Background:
    Given there is a chain "1" with sequencer "A"

  @sequencer @sbcp @periods
  Scenario: Triggers settlement immediately when StartPeriod arrives and no block is pending
    Given the sequencer "A" is at period ID "20" targeting superblock "11"
    And there is no open block for sequencer "A"
    And the last sealed block for period "9" has header:
      | field      | value  |
      | number     | 41     |
      | hash       | 0xabc1 |
      | state_root | 0x111  |
    When the sequencer "A" receives StartPeriod:
      | field               | value |
      | period_id           | 21    |
      | target_superblock   | 12    |
    Then the sequencer should update its period to "21" and its target superblock to "12"
    And it should request settlement proofs for period "20" targeting superblock "11" until block:
      | field      | value  |
      | number     | 41     |
      | hash       | 0xabc1 |
      | state_root | 0x111  |
    And it should send Proof(period_id="10", superblock="11") to the publisher

  @sequencer @sbcp @periods
  Scenario: Proof is sent to SP once received from prover
    Given the sequencer "A" has requested settlement proofs for period "20" targeting superblock "11"
    When the sequencer "A" receives Proof for:
      | field             | value        |
      | period_id         | 20           |
      | superblock_number | 11           |
      | proof_data        | 0xdeadbeef   |
    Then it should send to the SP the proof:
        | field             | value        |
        | period_id         | 20           |
        | superblock_number | 11           |
        | proof_data        | 0xdeadbeef   |

  @sequencer @sbcp @periods
  Scenario: Don't trigger settlement on StartPeriod when a block is pending
    Given the sequencer "A" has an open block "42" tagged with period "20" and superblock "11"
    When the sequencer "A" receives StartPeriod:
      | field               | value |
      | period_id           | 21    |
      | target_superblock   | 12    |
    Then the sequencer should update its period to "21" and its target superblock to "12"
    And no settlement pipeline should be triggered for period "20" yet


  @sequencer @sbcp @periods
  Scenario: Trigger settlement upon sealing the last block of a period
    Given the sequencer "A" is at period ID "21" targeting superblock "12"
    And the sequencer "A" has an open block "42" tagged with period "20" and superblock "11"
    When the sequencer "A" seals block "42" for period "20" with header:
      | field      | value  |
      | number     | 42     |
      | hash       | 0xdef2 |
      | state_root | 0x222  |
    And it should request settlement proofs for period "20" targeting superblock "11" until block:
      | field      | value  |
      | number     | 42     |
      | hash       | 0xdef2 |
      | state_root | 0x222  |

  @sequencer @sbcp @periods
  Scenario: Includes no header when no block was sealed in the previous period
    Given the sequencer "A" has no sealed block recorded for period "20"
    When the sequencer "A" receives StartPeriod:
      | field               | value |
      | period_id           | 21    |
      | target_superblock   | 12    |
    Then no proof request should be sent to the prover
    And the sequencer "A" should send its latest proof to the SP
