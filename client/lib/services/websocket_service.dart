import 'dart:async';
import 'dart:convert';
import 'package:web_socket_channel/web_socket_channel.dart';
import 'package:logger/logger.dart';
import '../models/models.dart';

enum ConnectionState { disconnected, connecting, connected, reconnecting }

class WebSocketService {
  final String baseUrl;
  final _logger = Logger();

  WebSocketChannel? _channel;
  String? _token;
  ConnectionState _state = ConnectionState.disconnected;

  // Stream controllers
  final _messageController = StreamController<Message>.broadcast();
  final _typingController = StreamController<Map<String, dynamic>>.broadcast();
  final _presenceController = StreamController<Map<String, dynamic>>.broadcast();
  final _inviteController = StreamController<Map<String, dynamic>>.broadcast();
  final _stateController = StreamController<ConnectionState>.broadcast();

  // Reconnection
  Timer? _reconnectTimer;
  int _reconnectAttempts = 0;
  static const _maxReconnectAttempts = 5;
  static const _reconnectDelay = Duration(seconds: 2);

  WebSocketService({required this.baseUrl});

  // Streams
  Stream<Message> get messageStream => _messageController.stream;
  Stream<Map<String, dynamic>> get typingStream => _typingController.stream;
  Stream<Map<String, dynamic>> get presenceStream => _presenceController.stream;
  Stream<Map<String, dynamic>> get inviteStream => _inviteController.stream;
  Stream<ConnectionState> get stateStream => _stateController.stream;
  ConnectionState get state => _state;

  void setToken(String token) => _token = token;

  Future<void> connect() async {
    if (_token == null) {
      throw Exception('Token not set');
    }

    _updateState(ConnectionState.connecting);

    try {
      final wsUrl = baseUrl.replaceFirst('http', 'ws');
      // Add token as query parameter for auth (more reliable than headers for WebSocket)
      final uri = Uri.parse('$wsUrl/ws').replace(queryParameters: {'token': _token});

      _channel = WebSocketChannel.connect(uri);

      _channel!.stream.listen(
        _handleMessage,
        onError: _handleError,
        onDone: _handleDone,
      );

      _updateState(ConnectionState.connected);
      _reconnectAttempts = 0;
      _logger.i('WebSocket connected');
    } catch (e) {
      _logger.e('WebSocket connection error: $e');
      _updateState(ConnectionState.disconnected);
      _scheduleReconnect();
    }
  }

  void _handleMessage(dynamic data) {
    try {
      final json = jsonDecode(data as String) as Map<String, dynamic>;
      final type = json['type'] as String?;
      final payload = json['payload'] as Map<String, dynamic>?;

      _logger.d('WS received: $type');

      switch (type) {
        case 'message.receive':
          if (payload != null) {
            // Extract the nested message and encryption data
            final messageJson = payload['message'] as Map<String, dynamic>?;
            _logger.d('WS message.receive payload keys: ${payload.keys}');
            _logger.d('encrypted_content type: ${payload['encrypted_content'].runtimeType}');
            _logger.d('nonce type: ${payload['nonce'].runtimeType}');
            if (messageJson != null) {
              // Add encrypted_content and nonce from outer payload to message
              // Server sends []byte which JSON-marshals as base64 string
              final encContent = payload['encrypted_content'];
              final nonceVal = payload['nonce'];
              _logger.d('encrypted_content value: $encContent');
              _logger.d('nonce value: $nonceVal');
              messageJson['encrypted_content'] = encContent is String ? encContent : null;
              messageJson['nonce'] = nonceVal is String ? nonceVal : null;
              _messageController.add(Message.fromJson(messageJson));
            }
          }
          break;
        case 'typing.start':
        case 'typing.stop':
          if (payload != null) {
            _typingController.add({
              'type': type,
              ...payload,
            });
          }
          break;
        case 'presence.update':
          if (payload != null) {
            _presenceController.add(payload);
          }
          break;
        case 'chat.invite':
        case 'chat.accepted':
        case 'user.left':
        case 'user.self_destructed':
          if (payload != null) {
            _inviteController.add({'type': type, ...payload});
          }
          break;
        case 'error':
          _logger.e('WS error: ${json['error']}');
          break;
      }
    } catch (e) {
      _logger.e('Error parsing WS message: $e');
    }
  }

  void _handleError(dynamic error) {
    _logger.e('WebSocket error: $error');
    _updateState(ConnectionState.disconnected);
    _scheduleReconnect();
  }

  void _handleDone() {
    _logger.w('WebSocket closed');
    _updateState(ConnectionState.disconnected);
    _scheduleReconnect();
  }

  void _scheduleReconnect() {
    if (_reconnectAttempts >= _maxReconnectAttempts) {
      _logger.e('Max reconnect attempts reached');
      return;
    }

    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(_reconnectDelay * (_reconnectAttempts + 1), () {
      _reconnectAttempts++;
      _updateState(ConnectionState.reconnecting);
      connect();
    });
  }

  void _updateState(ConnectionState newState) {
    _state = newState;
    _stateController.add(newState);
  }

  void _send(Map<String, dynamic> data) {
    if (_channel != null && _state == ConnectionState.connected) {
      _channel!.sink.add(jsonEncode(data));
    }
  }

  // Send encrypted message
  void sendMessage({
    required String chatId,
    required String encryptedContent,
    required String nonce,
    String contentType = 'text',
  }) {
    _send({
      'type': 'message.send',
      'payload': {
        'chat_id': chatId,
        'encrypted_content': encryptedContent,
        'content_type': contentType,
        'nonce': nonce,
      },
    });
  }

  // Typing indicators
  void startTyping(String chatId) {
    _send({
      'type': 'typing.start',
      'payload': {'chat_id': chatId},
    });
  }

  void stopTyping(String chatId) {
    _send({
      'type': 'typing.stop',
      'payload': {'chat_id': chatId},
    });
  }

  // Disconnect
  void disconnect() {
    _reconnectTimer?.cancel();
    _channel?.sink.close();
    _channel = null;
    _updateState(ConnectionState.disconnected);
    _logger.i('WebSocket disconnected');
  }

  // Cleanup
  void dispose() {
    disconnect();
    _messageController.close();
    _typingController.close();
    _presenceController.close();
    _inviteController.close();
    _stateController.close();
  }
}
