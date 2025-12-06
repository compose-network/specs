Feature: Protobuf wire encoding
  Each scenario defines a message instance and the expected
  protobuf wire encoding as a hexadecimal string.
  The implementation should construct the specified message and assert that
  its serialized bytes match the given hex.

  Scenario: HandshakeRequest basic serialization
    When the following "HandshakeRequest" message is serialized:
      | field      | format | value      |
      | timestamp  | int64  | 1234567890 |
      | public_key | hex    | abc        |
      | signature  | hex    | def        |
      | client_id  | string | client-1   |
      | nonce      | hex    | 123        |
    Then the wire hex should be "08d285d8cc041201ab1a01de2208636c69656e742d312a0112"

  Scenario: HandshakeResponse basic serialization
    When the following "HandshakeResponse" message is serialized:
      | field      | format | value         |
      | accepted   | bool   | true          |
      | error      | string | error-message |
      | session_id | string | session-123   |
    Then the wire hex should be "0801120d6572726f722d6d6573736167651a0b73657373696f6e2d313233"

  Scenario: Ping timestamp 42
    When the following "Ping" message is serialized:
      | field     | format | value |
      | timestamp | int64  | 42    |
    Then the wire hex should be "082a"

  Scenario: Pong timestamp 43
    When the following "Pong" message is serialized:
      | field     | format | value |
      | timestamp | int64  | 43    |
    Then the wire hex should be "082b"

  Scenario: TransactionRequest with two transactions
    When the following "TransactionRequest" message is serialized:
      | field        | format | value |
      | chain_id     | uint64 | 1     |
      | transaction0 | hex    | aa1   |
      | transaction1 | hex    | aa2   |
    Then the wire hex should be "08011201aa1201aa"

  Scenario: XTRequest with two transaction requests
    When the following "XTRequest" message is serialized:
      | field        | format | value                                           |
      | transaction0 | nested | TransactionRequest(chain_id=10, tx=[aa1])      |
      | transaction1 | nested | TransactionRequest(chain_id=11, tx=[aa2])      |
    Then the wire hex should be "0a05080a1201aa0a05080b1201aa"

  Scenario: StartInstance basic serialization
    When the following "StartInstance" message is serialized:
      | field           | format | value                                             |
      | instance_id     | hex    | 01                                                |
      | period_id       | uint64 | 100                                               |
      | sequence_number | uint64 | 7                                                 |
      | xt_request      | nested | XTRequest(transaction_requests=[TR(1, [aa1])])    |
    Then the wire hex should be "0a01011064180722070a0508011201aa"

  Scenario: Vote basic serialization
    When the following "Vote" message is serialized:
      | field       | format | value |
      | instance_id | hex    | 01    |
      | chain_id    | uint64 | 2     |
      | vote        | bool   | true  |
    Then the wire hex should be "0a010110021801"

  Scenario: Decided basic serialization
    When the following "Decided" message is serialized:
      | field       | format | value |
      | instance_id | hex    | 01    |
      | decision    | bool   | false |
    Then the wire hex should be "0a0101"

  Scenario: MailboxMessage with two data entries
    When the following "MailboxMessage" message is serialized:
      | field             | format | value    |
      | session_id        | uint64 | 123      |
      | instance_id       | hex    | 01       |
      | source_chain      | uint64 | 1        |
      | destination_chain | uint64 | 2        |
      | source            | hex    | adecb123 |
      | receiver          | hex    | 192bca   |
      | label             | string | label-1  |
      | data0             | hex    | aaabbb   |
    Then the wire hex should be "087b120101180120022a04adecb1233203192bca3a076c6162656c2d314203aaabbb"

  Scenario: StartPeriod basic serialization
    When the following "StartPeriod" message is serialized:
      | field             | format | value |
      | period_id         | uint64 | 5     |
      | superblock_number | uint64 | 42    |
    Then the wire hex should be "0805102a"

  Scenario: Rollback basic serialization
    When the following "Rollback" message is serialized:
      | field                          | format | value   |
      | period_id                      | uint64 | 6       |
      | last_finalized_superblock_num  | uint64 | 41      |
      | last_finalized_superblock_hash | hex    | 1123768 |
    Then the wire hex should be "080610291a03112376"

  Scenario: Proof basic serialization
    When the following "Proof" message is serialized:
      | field             | format | value |
      | period_id         | uint64 | 7     |
      | superblock_number | uint64 | 99    |
      | proof_data        | hex    | 156   |
    Then the wire hex should be "080710631a0115"

  Scenario: NativeDecided basic serialization
    When the following "NativeDecided" message is serialized:
      | field       | format | value |
      | instance_id | hex    | 01    |
      | decision    | bool   | true  |
    Then the wire hex should be "0a01011001"

  Scenario: WSDecided basic serialization
    When the following "WSDecided" message is serialized:
      | field       | format | value |
      | instance_id | hex    | 01    |
      | decision    | bool   | false |
    Then the wire hex should be "0a0101"

  Scenario: Message wrapper with HandshakeRequest payload
    When the following "Message" wrapper is serialized:
      | field     | format | value                                                                                      |
      | sender_id | string | sender-1                                                                                   |
      | payload   | nested | HandshakeRequest(timestamp=987654321, public_key=abc, signature=def, client_id=client-in-message, nonce=123) |
    Then the wire hex should be "0a0873656e6465722d31122208b1d1f9d6031201ab1a01de2211636c69656e742d696e2d6d6573736167652a0112"
