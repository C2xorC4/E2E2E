import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:logger/logger.dart';
import '../models/models.dart';

class ApiException implements Exception {
  final String message;
  final int? statusCode;
  ApiException(this.message, {this.statusCode});
  @override
  String toString() => 'ApiException: $message (status: $statusCode)';
}

class ApiService {
  final String baseUrl;
  final _logger = Logger();
  String? _token;

  ApiService({required this.baseUrl});

  void setToken(String token) => _token = token;
  void clearToken() => _token = null;
  bool get isAuthenticated => _token != null;

  Map<String, String> get _headers => {
        'Content-Type': 'application/json',
        if (_token != null) 'Authorization': 'Bearer $_token',
      };

  // Generic request that returns dynamic (can be Map or List)
  Future<dynamic> _requestDynamic(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, String>? queryParams,
  }) async {
    var uri = Uri.parse('$baseUrl$path');
    if (queryParams != null) {
      uri = uri.replace(queryParameters: queryParams);
    }

    _logger.d('$method $uri');

    http.Response response;
    switch (method) {
      case 'GET':
        response = await http.get(uri, headers: _headers);
        break;
      case 'POST':
        response = await http.post(uri, headers: _headers, body: jsonEncode(body));
        break;
      case 'PUT':
        response = await http.put(uri, headers: _headers, body: jsonEncode(body));
        break;
      case 'DELETE':
        response = await http.delete(uri, headers: _headers);
        break;
      default:
        throw ApiException('Unknown method: $method');
    }

    if (response.statusCode >= 200 && response.statusCode < 300) {
      if (response.body.isEmpty) return {};
      return jsonDecode(response.body);
    }

    String errorMsg = 'Request failed';
    try {
      final errorBody = jsonDecode(response.body);
      if (errorBody is Map) {
        errorMsg = errorBody['error'] ?? errorMsg;
      }
    } catch (_) {}

    throw ApiException(errorMsg, statusCode: response.statusCode);
  }

  // Request that expects a Map response
  Future<Map<String, dynamic>> _request(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, String>? queryParams,
  }) async {
    final result = await _requestDynamic(method, path, body: body, queryParams: queryParams);
    if (result is Map<String, dynamic>) {
      return result;
    }
    // If the response is not a Map, wrap it
    return {'data': result};
  }

  // Request that expects a List response
  Future<List<dynamic>> _requestList(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, String>? queryParams,
  }) async {
    final result = await _requestDynamic(method, path, body: body, queryParams: queryParams);
    if (result is List) {
      return result;
    }
    return [];
  }

  // ============ Auth ============

  Future<Map<String, dynamic>> register({
    required String username,
    required String email,
    required String password,
    required String publicKey,
    required String deviceName,
    required String deviceKey,
    Map<String, dynamic>? deviceProfile,
  }) async {
    final body = <String, dynamic>{
      'username': username,
      'email': email,
      'password': password,
      'public_key': publicKey,
      'device_name': deviceName,
      'device_key': deviceKey,
    };

    if (deviceProfile != null) {
      body['device_profile'] = deviceProfile;
    }

    final result = await _request('POST', '/api/auth/register', body: body);

    if (result['token'] != null) {
      _token = result['token'];
    }
    return result;
  }

  Future<Map<String, dynamic>> login({
    required String email,
    required String password,
    required String deviceName,
    required String deviceKey,
    Map<String, dynamic>? deviceProfile,
  }) async {
    final body = <String, dynamic>{
      'email': email,
      'password': password,
      'device_name': deviceName,
      'device_key': deviceKey,
    };

    if (deviceProfile != null) {
      body['device_profile'] = deviceProfile;
    }

    final result = await _request('POST', '/api/auth/login', body: body);

    if (result['token'] != null) {
      _token = result['token'];
    }
    return result;
  }

  Future<void> logout() async {
    await _request('POST', '/api/auth/logout');
    _token = null;
  }

  // ============ Users ============

  Future<List<User>> searchUsers(String query, {int limit = 20}) async {
    final result = await _requestList('GET', '/api/users/search',
        queryParams: {'q': query, 'limit': limit.toString()});
    return result.map((u) => User.fromJson(u as Map<String, dynamic>)).toList();
  }

  Future<User> getUser(String userId) async {
    final result = await _request('GET', '/api/users/$userId');
    return User.fromJson(result);
  }

  Future<Map<String, dynamic>> getUserKeys(String userId) async {
    return await _request('GET', '/api/users/$userId/keys');
  }

  // ============ Chats ============

  Future<List<Chat>> getChats() async {
    final result = await _requestList('GET', '/api/chats');
    return result.map((c) => Chat.fromJson(c as Map<String, dynamic>)).toList();
  }

  Future<Chat> createChat({
    required List<String> participantIds,
    bool isGroup = false,
    String? name,
  }) async {
    final result = await _request('POST', '/api/chats', body: {
      'participants': participantIds,
      'is_group': isGroup,
      if (name != null) 'name': name,
    });
    return Chat.fromJson(result);
  }

  Future<Chat> createDirectChat(String participantId) async {
    return createChat(participantIds: [participantId], isGroup: false);
  }

  Future<Chat> createGroupChat(String name, List<String> participantIds) async {
    return createChat(participantIds: participantIds, isGroup: true, name: name);
  }

  Future<Map<String, dynamic>> getChat(String chatId) async {
    return await _request('GET', '/api/chats/$chatId');
  }

  Future<void> addParticipant(String chatId, String userId) async {
    await _request('POST', '/api/chats/$chatId/participants', body: {
      'user_id': userId,
    });
  }

  // ============ Messages ============

  Future<List<Message>> getMessages(String chatId, {int limit = 50, int offset = 0}) async {
    final result = await _requestList('GET', '/api/chats/$chatId/messages',
        queryParams: {'limit': limit.toString(), 'offset': offset.toString()});
    return result.map((m) => Message.fromJson(m as Map<String, dynamic>)).toList();
  }

  Future<List<Message>> searchMessages(String query, {int limit = 50}) async {
    final result = await _requestList('GET', '/api/messages/search',
        queryParams: {'q': query, 'limit': limit.toString()});
    return result.map((m) => Message.fromJson(m as Map<String, dynamic>)).toList();
  }

  // ============ Participants ============

  Future<List<User>> getChatParticipants(String chatId) async {
    final result = await _request('GET', '/api/chats/$chatId');
    final participants = result['participants'];
    if (participants is List) {
      return participants.map((p) => User.fromJson(p as Map<String, dynamic>)).toList();
    }
    return [];
  }

  Future<void> inviteToChat(String chatId, String userId) async {
    await _request('POST', '/api/chats/$chatId/participants', body: {
      'user_id': userId,
    });
  }

  // ============ Invites ============

  Future<List<Map<String, dynamic>>> getPendingInvites() async {
    final result = await _requestList('GET', '/api/invites');
    return result.map((i) => i as Map<String, dynamic>).toList();
  }

  Future<void> acceptInvite(String chatId) async {
    await _request('POST', '/api/invites/$chatId/accept');
  }

  Future<void> declineInvite(String chatId) async {
    await _request('POST', '/api/invites/$chatId/decline');
  }

  // ============ Leave/Self-destruct ============

  Future<void> leaveChat(String chatId) async {
    await _request('POST', '/api/chats/$chatId/leave');
  }

  Future<void> selfDestructChat(String chatId) async {
    await _request('POST', '/api/chats/$chatId/self-destruct');
  }

  // ============ Device Profile ============

  Future<Map<String, dynamic>> getDeviceProfile() async {
    return await _request('GET', '/api/device/profile');
  }

  Future<void> updateDeviceProfile(Map<String, dynamic> profile) async {
    await _request('PUT', '/api/device/profile', body: profile);
  }

  Future<void> updateMeshSettings({
    bool? meshEnabled,
    String? bluetoothAddress,
    String? wifiDirectAddress,
  }) async {
    final body = <String, dynamic>{};
    if (meshEnabled != null) body['mesh_enabled'] = meshEnabled;
    if (bluetoothAddress != null) body['bluetooth_address'] = bluetoothAddress;
    if (wifiDirectAddress != null) body['wifi_direct_address'] = wifiDirectAddress;
    await _request('PUT', '/api/device/mesh', body: body);
  }
}
