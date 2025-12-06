Feature: Publisher Instance Scheduling
  The SP initiates multiple composability instances during a period.
  It should maintain a queue with incoming XTRequests from users
  (either sent directly from the user, or relayed by sequencers).
  It follows the constrain that no chain can participate in more than one
  active instance at the same time.
  Thus, parallel instances are allowed at the publisher level
  as long as their participant sets are disjoint.
  Moreover, each instance is assigned a sequence number that increases
  monotonically within a period, starting from 1.
  Once a new period starts, the sequence number resets to 1.
  When an instance is decided, its participant chains are freed
  and can be included in future instances.

  Background:
    Given the participating chains are "1,2,3,4"
    And SP is currently at period ID "20" targeting superblock "8"

  @publisher @sbcp @instances
  Scenario: Starts an instance with valid XTRequest and starts from sequence number 1
    Given SP has not started any instance in period "20"
    And chains "1,2" are inactive
    When SP attempts to start an instance with XTRequest:
      """
      1: [tx1_a, tx1_b]
      2: [tx2]
      """
    Then SP should create an instance:
        | field           | value |
        | sequence_number | 1     |
        | period_id       | 20     |
        | xt_request      | "1: [tx1_a, tx1_b] 2: [tx2]" |
    And chains "1,2" should be marked as active

  @publisher @sbcp @instances
  Scenario: Starts multiple instances with disjoint participants
    Given SP started an instance:
      | field           | value |
      | sequence_number | 1     |
      | period_id       | 20     |
      | xt_request      | "1: [tx1_a, tx1_b] 2: [tx2]" |
    And chains "3,4" are inactive
    When SP attempts to start an instance with XTRequest:
      """
      3: [tx3]
      4: [tx4]
      """
    Then SP should create an instance:
        | field           | value |
        | sequence_number | 2     |
        | period_id       | 20     |
        | xt_request      | "3: [tx3] 4: [tx4]" |
    And chains "1,2" should be marked as active
    And chains "3,4" should be marked as active

  @publisher @sbcp @instances
  Scenario: Rejects XTRequests that overlap with active participants
    Given chains "1,2" are active
    When SP attempts to start an instance with XTRequest:
      """
      2: [tx2]
      3: [tx3]
      """
    Then the attempt should fail with error:
      """
      can not start any instance
      """
    And chains "1,2" should be marked as active
    And chain "3" should be marked as inactive
    And the following XTRequest should remain in the queue:
      """
      2: [tx2]
      3: [tx3]
      """

  @publisher @sbcp @instances
  Scenario: Validates that requests span at least two chains
    Given chains "1" are inactive
    When SP attempts to start an instance with XTRequest:
      """
      1: [tx]
      """
    Then the attempt should fail with error:
      """
      invalid request
      """
    And chain "1" should be marked as inactive

  @publisher @sbcp @instances
  Scenario Outline: Sequence numbers increase monotonically within the same period
    Given the last instance SP started was in period "20" with sequence number <last>
    And chains "3,4" are inactive
    When SP attempts to start an instance with XTRequest:
      """
      3: [tx3]
      4: [tx4]
      """
    Then SP should create an instance:
        | field           | value |
        | sequence_number | <new> |
        | period_id       | 20    |
        | xt_request      | "3: [tx3] 4: [tx4]" |
    And chains "3,4" should be marked as active

    Examples:
      | last | new |
      | 1    | 2   |
      | 2    | 3   |
      | 5    | 6   |

  @publisher @sbcp @instances
  Scenario Outline: Sequence numbers reset at the start of a new period
    Given the last instance SP started was in period "20" with sequence number <last>
    And SP is currently at period ID "21" targeting superblock "9"
    And chains "1,2" are inactive
    When SP attempts to start an instance with XTRequest:
      """
      1: [tx1]
      2: [tx2]
      """
    Then SP should create an instance:
        | field           | value |
        | sequence_number | 1     |
        | period_id       | 21    |
        | xt_request      | "1: [tx1] 2: [tx2]" |
    And chains "1,2" should be marked as active

    Examples:
      | last |
      | 1    |
      | 3    |
      | 10   |

  @publisher @sbcp @instances
  Scenario: Deciding an instance frees its chains for future requests
    Given chains "1,2" are active for instance "0xabc"
    When SP decides instance "0xabc"
    Then chains "1,2" should be marked as inactive
