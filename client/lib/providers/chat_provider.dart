import 'dart:async';
import 'package:flutter/foundation.dart';
import 'package:uuid/uuid.dart';
import '../models/models.dart';
import '../services/services.dart';

class ChatProvider extends ChangeNotifier {
  final ApiService _api;
  final CryptoService _crypto;
  final WebSocketService _ws;
  final CacheService? _cache;
  final String _currentUserId;

  List<Chat> _chats = [];
  Chat? _selectedChat;
  Map<String, List<Message>> _messages = {};
  Map<String, bool> _typingUsers = {};
  List<Map<String, dynamic>> _pendingInvites = [];
  bool _isLoading = false;
  String? _error;

  StreamSubscription? _messageSubscription;
  StreamSubscription? _typingSubscription;
  StreamSubscription? _inviteSubscription;

  ChatProvider({
    required ApiService api,
    required CryptoService crypto,
    required WebSocketService ws,
    required String currentUserId,
    CacheService? cache,
  })  : _api = api,
        _crypto = crypto,
        _ws = ws,
        _currentUserId = currentUserId,
        _cache = cache {
    _setupListeners();
  }

  List<Chat> get chats => _chats;
  Chat? get selectedChat => _selectedChat;
  List<Message> get currentMessages => _messages[_selectedChat?.id] ?? [];
  Map<String, bool> get typingUsers => _typingUsers;
  List<Map<String, dynamic>> get pendingInvites => _pendingInvites;
  int get pendingInviteCount => _pendingInvites.length;
  bool get isLoading => _isLoading;
  String? get error => _error;
  bool get isCacheEnabled => _cache != null && _cache!.isInitialized;

  void _setupListeners() {
    _messageSubscription = _ws.messageStream.listen(_handleNewMessage);
    _typingSubscription = _ws.typingStream.listen(_handleTyping);
    _inviteSubscription = _ws.inviteStream.listen(_handleInvite);
  }

  void _handleInvite(Map<String, dynamic> data) {
    final type = data['type'] as String?;

    switch (type) {
      case 'chat.invite':
        // A new invite arrived - add to pending list
        _pendingInvites.insert(0, data);
        notifyListeners();
        break;
      case 'chat.accepted':
      case 'user.left':
      case 'user.self_destructed':
        // Someone accepted/left/self-destructed - refresh chat list
        loadChats();
        loadPendingInvites();
        break;
    }
  }

  void _handleNewMessage(Message message) async {
    // Decrypt if needed
    Message decryptedMessage = message;
    if (message.encryptedContent != null && message.nonce != null) {
      try {
        final content = await _crypto.decrypt(
          message.encryptedContent!,
          message.nonce!,
        );
        decryptedMessage = message.copyWith(content: content);
      } catch (e) {
        decryptedMessage = message.copyWith(content: '[Decryption failed]');
      }
    }

    // Add to messages
    final chatMessages = _messages[message.chatId] ?? [];
    _messages[message.chatId] = [decryptedMessage, ...chatMessages];

    // Cache the decrypted message
    await _cacheMessage(decryptedMessage);

    // Update chat list
    final chatIndex = _chats.indexWhere((c) => c.id == message.chatId);
    if (chatIndex != -1) {
      final chat = _chats[chatIndex];
      _chats[chatIndex] = Chat(
        id: chat.id,
        name: chat.name,
        isGroup: chat.isGroup,
        createdAt: chat.createdAt,
        participants: chat.participants,
        lastMessage: decryptedMessage,
        unreadCount: _selectedChat?.id == chat.id ? 0 : chat.unreadCount + 1,
      );
      // Move to top
      _chats.sort((a, b) {
        final aTime = a.lastMessage?.createdAt ?? a.createdAt;
        final bTime = b.lastMessage?.createdAt ?? b.createdAt;
        return bTime.compareTo(aTime);
      });
    }

    notifyListeners();
  }

  void _handleTyping(Map<String, dynamic> data) {
    final chatId = data['chat_id'] as String?;
    final userId = data['user_id'] as String?;
    final type = data['type'] as String?;

    if (chatId == null || userId == null) return;
    if (userId == _currentUserId) return; // Ignore own typing

    final key = '$chatId:$userId';
    _typingUsers[key] = type == 'typing.start';
    notifyListeners();

    // Auto-clear after 5 seconds
    if (type == 'typing.start') {
      Future.delayed(const Duration(seconds: 5), () {
        _typingUsers.remove(key);
        notifyListeners();
      });
    }
  }

  /// Cache a message using the encrypted cache service
  Future<void> _cacheMessage(Message message) async {
    if (_cache == null || !_cache!.isInitialized) return;

    try {
      await _cache!.saveMessage(message);
    } catch (e) {
      debugPrint('Failed to cache message: $e');
      // Don't throw - caching failure shouldn't break the app
    }
  }

  /// Cache multiple messages
  Future<void> _cacheMessages(List<Message> messages) async {
    if (_cache == null || !_cache!.isInitialized) return;
    if (messages.isEmpty) return;

    try {
      await _cache!.saveMessages(messages);
    } catch (e) {
      debugPrint('Failed to cache messages: $e');
    }
  }

  /// Load messages from cache for a chat
  Future<List<Message>> _loadFromCache(String chatId) async {
    if (_cache == null || !_cache!.isInitialized) return [];

    try {
      return await _cache!.getMessages(chatId);
    } catch (e) {
      debugPrint('Failed to load messages from cache: $e');
      return [];
    }
  }

  Future<void> loadChats() async {
    _isLoading = true;
    _error = null;
    notifyListeners();

    try {
      _chats = await _api.getChats();
      _chats.sort((a, b) {
        final aTime = a.lastMessage?.createdAt ?? a.createdAt;
        final bTime = b.lastMessage?.createdAt ?? b.createdAt;
        return bTime.compareTo(aTime);
      });
    } catch (e) {
      _error = e.toString();
    }

    _isLoading = false;
    notifyListeners();
  }

  Future<void> selectChat(Chat chat) async {
    _selectedChat = chat;
    notifyListeners();

    if (!_messages.containsKey(chat.id)) {
      await loadMessages(chat.id);
    }
  }

  void clearSelection() {
    _selectedChat = null;
    notifyListeners();
  }

  Future<void> loadMessages(String chatId, {bool refresh = false}) async {
    if (!refresh && _messages.containsKey(chatId)) return;

    _isLoading = true;
    notifyListeners();

    try {
      // Try loading from cache first for faster UI response
      if (!refresh) {
        final cachedMessages = await _loadFromCache(chatId);
        if (cachedMessages.isNotEmpty) {
          _messages[chatId] = cachedMessages;
          notifyListeners();
        }
      }

      // Fetch from server
      final messages = await _api.getMessages(chatId);

      // Decrypt messages
      final decrypted = <Message>[];
      for (final msg in messages) {
        if (msg.encryptedContent != null && msg.nonce != null) {
          try {
            final content = await _crypto.decrypt(
              msg.encryptedContent!,
              msg.nonce!,
            );
            decrypted.add(msg.copyWith(content: content));
          } catch (e) {
            decrypted.add(msg.copyWith(content: '[Decryption failed]'));
          }
        } else {
          decrypted.add(msg);
        }
      }

      _messages[chatId] = decrypted;

      // Cache the decrypted messages
      await _cacheMessages(decrypted);
    } catch (e) {
      _error = e.toString();
      // If server fetch fails, try to use cached messages
      if (!_messages.containsKey(chatId) || _messages[chatId]!.isEmpty) {
        final cachedMessages = await _loadFromCache(chatId);
        if (cachedMessages.isNotEmpty) {
          _messages[chatId] = cachedMessages;
        }
      }
    }

    _isLoading = false;
    notifyListeners();
  }

  Future<bool> sendMessage(String content) async {
    if (_selectedChat == null) return false;

    final chatId = _selectedChat!.id;

    try {
      // Encrypt message
      final encrypted = await _crypto.encrypt(content);

      // Create optimistic message
      final tempId = const Uuid().v4();
      final optimisticMessage = Message(
        id: tempId,
        chatId: chatId,
        senderId: _currentUserId,
        content: content,
        createdAt: DateTime.now(),
        status: MessageStatus.sending,
      );

      // Add to local messages
      final chatMessages = _messages[chatId] ?? [];
      _messages[chatId] = [optimisticMessage, ...chatMessages];
      notifyListeners();

      // Cache the optimistic message
      await _cacheMessage(optimisticMessage);

      // Send via WebSocket
      _ws.sendMessage(
        chatId: chatId,
        encryptedContent: encrypted['encrypted_content']!,
        nonce: encrypted['nonce']!,
      );

      return true;
    } catch (e) {
      _error = e.toString();
      notifyListeners();
      return false;
    }
  }

  void sendTyping() {
    if (_selectedChat != null) {
      _ws.startTyping(_selectedChat!.id);
    }
  }

  void stopTyping() {
    if (_selectedChat != null) {
      _ws.stopTyping(_selectedChat!.id);
    }
  }

  Future<Chat?> createChat(List<String> participantIds, {String? name, bool isGroup = false}) async {
    try {
      final chat = await _api.createChat(
        participantIds: participantIds,
        name: name,
        isGroup: isGroup,
      );
      await loadChats();
      return chat;
    } catch (e) {
      _error = e.toString();
      notifyListeners();
      return null;
    }
  }

  Future<Chat?> createGroupChat(String name, List<String> participantIds) async {
    return createChat(participantIds, name: name, isGroup: true);
  }

  Future<bool> inviteToChat(String chatId, String userId) async {
    try {
      await _api.inviteToChat(chatId, userId);
      // Reload chat list to get updated participants
      await loadChats();
      // Also reload the current chat if it's the one we invited to
      if (_selectedChat?.id == chatId) {
        final updatedChat = _chats.firstWhere(
          (c) => c.id == chatId,
          orElse: () => _selectedChat!,
        );
        _selectedChat = updatedChat;
        notifyListeners();
      }
      return true;
    } catch (e) {
      _error = e.toString();
      notifyListeners();
      return false;
    }
  }

  bool get isGroupChat => _selectedChat?.isGroup ?? false;

  List<User> get chatParticipants => _selectedChat?.participants ?? [];

  // ============ Invites ============

  Future<void> loadPendingInvites() async {
    try {
      _pendingInvites = await _api.getPendingInvites();
      notifyListeners();
    } catch (e) {
      _error = e.toString();
      notifyListeners();
    }
  }

  Future<bool> acceptInvite(String chatId) async {
    try {
      await _api.acceptInvite(chatId);
      // Remove from pending list
      _pendingInvites.removeWhere((invite) {
        final chat = invite['chat'] as Map<String, dynamic>?;
        return chat?['id'] == chatId;
      });
      // Reload chats to include the newly accepted chat
      await loadChats();
      notifyListeners();
      return true;
    } catch (e) {
      _error = e.toString();
      notifyListeners();
      return false;
    }
  }

  Future<bool> declineInvite(String chatId) async {
    try {
      await _api.declineInvite(chatId);
      // Remove from pending list
      _pendingInvites.removeWhere((invite) {
        final chat = invite['chat'] as Map<String, dynamic>?;
        return chat?['id'] == chatId;
      });
      notifyListeners();
      return true;
    } catch (e) {
      _error = e.toString();
      notifyListeners();
      return false;
    }
  }

  // ============ Leave/Self-destruct ============

  Future<bool> leaveChat(String chatId) async {
    try {
      await _api.leaveChat(chatId);
      // Remove chat from local list immediately
      _chats.removeWhere((c) => c.id == chatId);
      // Remove messages from memory cache
      _messages.remove(chatId);
      // Remove messages from persistent cache
      await _clearChatFromCache(chatId);
      // Also remove from pending invites if it was there
      _pendingInvites.removeWhere((invite) {
        final chat = invite['chat'] as Map<String, dynamic>?;
        return chat?['id'] == chatId;
      });
      // Clear selection if this was the selected chat
      if (_selectedChat?.id == chatId) {
        _selectedChat = null;
      }
      notifyListeners();
      // Refresh both chats and pending invites from server to ensure sync
      await Future.wait([
        loadChats(),
        loadPendingInvites(),
      ]);
      return true;
    } catch (e) {
      _error = e.toString();
      notifyListeners();
      return false;
    }
  }

  Future<bool> selfDestructChat(String chatId) async {
    try {
      await _api.selfDestructChat(chatId);
      // Remove chat from local list immediately
      _chats.removeWhere((c) => c.id == chatId);
      // Remove messages from memory cache - this clears local storage
      _messages.remove(chatId);
      // Remove messages from persistent cache
      await _clearChatFromCache(chatId);
      // Also remove from pending invites if it was there
      _pendingInvites.removeWhere((invite) {
        final chat = invite['chat'] as Map<String, dynamic>?;
        return chat?['id'] == chatId;
      });
      // Clear selection if this was the selected chat
      if (_selectedChat?.id == chatId) {
        _selectedChat = null;
      }
      notifyListeners();
      // Refresh both chats and pending invites from server to ensure sync
      await Future.wait([
        loadChats(),
        loadPendingInvites(),
      ]);
      return true;
    } catch (e) {
      _error = e.toString();
      notifyListeners();
      return false;
    }
  }

  /// Clear cached messages for a specific chat
  Future<void> _clearChatFromCache(String chatId) async {
    if (_cache == null || !_cache!.isInitialized) return;

    try {
      await _cache!.clearChatCache(chatId);
    } catch (e) {
      debugPrint('Failed to clear chat from cache: $e');
    }
  }

  /// Clear all cached messages
  Future<void> clearAllCache() async {
    if (_cache == null || !_cache!.isInitialized) return;

    try {
      await _cache!.clearCache();
    } catch (e) {
      debugPrint('Failed to clear cache: $e');
    }
  }

  /// Get cache statistics
  Future<Map<String, dynamic>?> getCacheStats() async {
    if (_cache == null || !_cache!.isInitialized) return null;

    try {
      return await _cache!.getCacheStats();
    } catch (e) {
      debugPrint('Failed to get cache stats: $e');
      return null;
    }
  }

  @override
  void dispose() {
    _messageSubscription?.cancel();
    _typingSubscription?.cancel();
    _inviteSubscription?.cancel();
    super.dispose();
  }
}
