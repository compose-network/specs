Feature: Publisher Settlement Proofs
  Once a period ends, sequencers produce zk proofs for their respective chains and
  submit them to the SP
  The SP collects the proofs in a sequential manner, respecting the order of superblocks.
  Thus, only proofs for the next superblock to be proven (the one after the last finalized superblock) are accepted.
  Once all proofs for the next superblock to be proven are collected, the SP requests
  a superblock proof from the prover, and publishes it to L1.
  A request error triggers a rollback to the last finalized superblock.

  Background:
    Given the participating chains are "1,2"
    And the last finalized superblock is:
      | field  | value |
      | number | 5     |
      | hash   | 0x999 |
    And SP is currently at period ID "10" targeting superblock "7"

  @publisher @sbcp @proofs
  Scenario: Request proof once all per-chain proofs are collected
    Given SP received Proof from chain "1" for:
      | field             | value |
      | period_id         | 9     |
      | superblock_number | 6     |
      | proof_data        | 0xaa  |
    When SP receives Proof from chain "2" for:
      | field             | value |
      | period_id         | 9     |
      | superblock_number | 6     |
      | proof_data        | 0xbb  |
    Then SP should request the superblock proof:
        | field             | value |
        | superblock_number | 6     |
        | last_finalized_hash | 0x999 |
        | proofs            | "1:0xaa, 2:0xbb" |
    And it should publish the resulting proof to L1 for superblock "6"
    And the cached per-chain proofs for superblock "6" should be discarded

  @publisher @sbcp @proofs
  Scenario: Duplicate proofs from the same chain are ignored
    Given SP received Proof from chain "1" for:
      | field             | value |
      | period_id         | 9     |
      | superblock_number | 6     |
      | proof_data        | 0xaa  |
    When SP receives Proof from chain "1" for:
      | field             | value |
      | period_id         | 9     |
      | superblock_number | 6     |
      | proof_data        | 0xbb  |
    Then the proof should be ignored

  @publisher @sbcp @proofs
  Scenario: Upon successful request, the superblock proof is published to L1
    Given SP requested a superblock proof for superblock "6"
    When SP receives a successful response with a superblock proof "0xabc" for superblock "6"
    Then SP should publish the proof "0xabc" to L1 for superblock "6"

  @publisher @sbcp @proofs
  Scenario Outline: Ignores proofs that do not belong to the next superblock to be proven
    When SP receives Proof from chain "1" for:
      | field             | value        |
      | period_id         | <period_id>  |
      | superblock_number | <superblock> |
      | proof_data        | 0xcc         |
    Then the proof should be ignored

    Examples:
      | period_id | superblock |
      | 7         | 4          |
      | 10        | 7          |
      | 8         | 5          |
      | 8         | 6          |

  @publisher @sbcp @proofs @rollback
  Scenario: Rolls back when the proof request fails
    Given SP requested a superblock proof for superblock "6"
    When the prover fails to generate the superblock proof for superblock "6"
    Then SP should broadcast a Rollback with:
      | field        | value |
      | period_id    | 10    |
      | superblock   | 5     |
      | hash         | 0x999 |
    And the target superblock should reset to "6"

  @publisher @sbcp @proofs @rollback
  Scenario: Proof timeout clears cached proofs and active instances
    Given chains "1,2" are active
    And SP received Proof from chain "1" for:
      | field             | value |
      | period_id         | 9     |
      | superblock_number | 6     |
      | proof_data        | 0xaa  |
    And SP received Proof from chain "2" for:
      | field             | value |
      | period_id         | 9     |
      | superblock_number | 6     |
      | proof_data        | 0xbb  |
    When SP triggers a proof timeout
    Then SP should broadcast a Rollback with:
      | field        | value |
      | period_id    | 10    |
      | superblock   | 5     |
      | hash         | 0x999 |
    And chains "1,2" should be marked as inactive
    And the target superblock should reset to "6"
    And the cached per-chain proofs for superblock "6" should be discarded

  @publisher @sbcp @proofs
  Scenario: Advances the settled state monotonically
    When SP receives the event that superblock "6" with hash "0x777" has been finalized
    Then the last finalized superblock should update to "6" with hash "0x777"

  @publisher @sbcp @proofs
  Scenario: Settlement event for old superblock is ignored
    When SP receives the event that superblock "5" with hash "0x999" has been finalized
    Then the event should be ignored
