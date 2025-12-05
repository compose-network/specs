Feature: Sequencer Start Instance
  For a sequencer, an instance starts when it receives a StartInstance message from the SP.
  On the instance startup, the sequencer should start a timer for it, and filter the transactions
  that belong to its own chain for simulation.
  In case there are no transactions for its own chain, the sequencer should reject the instance immediately
  as it shouldn't have received the message and this consists of an invalid protocol state.
  After startup, the sequencer should immediately start simulating the transactions for the instance.

  Background:
    Given there is a chain "1" with sequencer "A"
    And there is a chain "2" with sequencer "B"

  @start-instance
  Scenario: Filters local transactions and starts a timer
    When sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 1
      sequence_number: 1
      xtrequest:
        1: [tx1_1, tx1_2]
        2: [tx2]
      """
    Then sequencer "A" should only consider transactions "[tx1_1,tx1_2]" for simulation
    And a timer for instance "0x1" should start
    And simulations for transactions "[tx1_1,tx1_2]" should start

  @start-instance
  Scenario: Rejects StartInstance with no local transactions
    When sequencer "A" receives StartInstance:
      """
      instance_id: 0x1
      period_id: 1
      sequence_number: 1
      xtrequest:
        2: [tx2]
        3: [tx3]
      """
    Then an error occurs:
      """
      No transactions for this chain
      """
