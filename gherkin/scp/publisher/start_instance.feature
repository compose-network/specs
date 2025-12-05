Feature: Publisher Start Instance
  The shared publisher is responsible for launching every SCP instance.
  When an instance starts, it broadcasts the StartInstance message to each rollup that
  appears in the XTRequest and starts a local timeout for the decision phase.

  Background:
    Given there is a shared publisher "SP"
    And there is a chain "1" with sequencer "A"
    And there is a chain "2" with sequencer "B"
    And there is a chain "3" with sequencer "C"
    And there is a chain "4" with sequencer "D"

  @publisher @scp @start-instance
  Scenario: Broadcasts StartInstance to all participating sequencers
    When the shared publisher "SP" starts an instance:
      """
      instance_id: 0x1
      period_id: 7
      sequence_number: 3
      xtrequest:
        1: [tx1_1, tx1_2]
        2: [tx2]
        3: [tx3]
      """
    Then sequencers "A,B,C" should receive StartInstance:
      """
      instance_id: 0x1
      period_id: 7
      sequence_number: 3
      xtrequest:
        1: [tx1_1, tx1_2]
        2: [tx2]
        3: [tx3]
      """
    And a timer for instance "0x1" should start at the shared publisher

  @publisher @scp @start-instance
  Scenario: Notifies only chains referenced in the XTRequest
    When the shared publisher "SP" starts an instance:
      """
      instance_id: 0x2
      period_id: 8
      sequence_number: 4
      xtrequest:
        1: [tx1]
        2: [tx2]
      """
    Then sequencer "A" should receive StartInstance with instance ID "0x2"
    And sequencer "B" should receive StartInstance with instance ID "0x2"
    And sequencer "C" should not receive StartInstance for instance "0x2"
    And sequencer "D" should not receive StartInstance for instance "0x2"
