import 'dart:convert';
import 'package:logger/logger.dart';
import 'package:path/path.dart';
import 'package:path_provider/path_provider.dart';
import 'package:sqflite/sqflite.dart';
import '../models/message.dart';
import 'crypto_service.dart';

/// Exception thrown when cache operations fail
class CacheException implements Exception {
  final String message;
  final dynamic originalError;

  CacheException(this.message, [this.originalError]);

  @override
  String toString() => 'CacheException: $message';
}

/// Service for encrypted local message caching
/// Uses SQLite for storage with XChaCha20-Poly1305 encryption
class CacheService {
  final _logger = Logger();
  final CryptoService _crypto;

  Database? _database;
  bool _isInitialized = false;

  // Database constants
  static const _dbName = 'encrypted_cache.db';
  static const _dbVersion = 1;

  // Table names
  static const _messagesTable = 'messages';

  CacheService({required CryptoService crypto}) : _crypto = crypto;

  /// Initialize the cache database
  Future<void> init() async {
    if (_isInitialized) return;

    if (!_crypto.hasCacheKey) {
      throw CacheException(
        'Cannot initialize cache without encryption key. '
        'Ensure device key pair is set in CryptoService first.',
      );
    }

    try {
      final documentsDirectory = await getApplicationDocumentsDirectory();
      final dbPath = join(documentsDirectory.path, _dbName);

      _database = await openDatabase(
        dbPath,
        version: _dbVersion,
        onCreate: _onCreate,
        onUpgrade: _onUpgrade,
      );

      _isInitialized = true;
      _logger.i('Cache service initialized');
    } catch (e) {
      _logger.e('Failed to initialize cache database', error: e);
      throw CacheException('Failed to initialize cache database', e);
    }
  }

  /// Create database tables
  Future<void> _onCreate(Database db, int version) async {
    await db.execute('''
      CREATE TABLE $_messagesTable (
        id TEXT PRIMARY KEY,
        chat_id TEXT NOT NULL,
        sender_id TEXT NOT NULL,
        encrypted_content TEXT NOT NULL,
        nonce TEXT NOT NULL,
        content_type TEXT NOT NULL,
        created_at INTEGER NOT NULL,
        edited_at INTEGER,
        status TEXT NOT NULL,
        UNIQUE(id)
      )
    ''');

    // Create index for efficient chat queries
    await db.execute('''
      CREATE INDEX idx_messages_chat_id ON $_messagesTable(chat_id)
    ''');

    await db.execute('''
      CREATE INDEX idx_messages_created_at ON $_messagesTable(created_at)
    ''');

    _logger.d('Cache database tables created');
  }

  /// Handle database upgrades
  Future<void> _onUpgrade(Database db, int oldVersion, int newVersion) async {
    // Handle future migrations here
    _logger.d('Cache database upgraded from $oldVersion to $newVersion');
  }

  /// Ensure database is initialized
  void _ensureInitialized() {
    if (!_isInitialized || _database == null) {
      throw CacheException('Cache service not initialized. Call init() first.');
    }
  }

  /// Save a message to the encrypted cache
  /// The message content is encrypted before storage
  Future<void> saveMessage(Message message) async {
    _ensureInitialized();

    try {
      // Encrypt the message content
      final encrypted = await _crypto.encryptForCache(message.content);

      await _database!.insert(
        _messagesTable,
        {
          'id': message.id,
          'chat_id': message.chatId,
          'sender_id': message.senderId,
          'encrypted_content': encrypted['encrypted_content'],
          'nonce': encrypted['nonce'],
          'content_type': message.contentType,
          'created_at': message.createdAt.millisecondsSinceEpoch,
          'edited_at': message.editedAt?.millisecondsSinceEpoch,
          'status': message.status.name,
        },
        conflictAlgorithm: ConflictAlgorithm.replace,
      );

      _logger.d('Message ${message.id} saved to cache');
    } catch (e) {
      _logger.e('Failed to save message to cache', error: e);
      throw CacheException('Failed to save message to cache', e);
    }
  }

  /// Save multiple messages to the encrypted cache
  Future<void> saveMessages(List<Message> messages) async {
    _ensureInitialized();

    if (messages.isEmpty) return;

    try {
      await _database!.transaction((txn) async {
        final batch = txn.batch();

        for (final message in messages) {
          final encrypted = await _crypto.encryptForCache(message.content);

          batch.insert(
            _messagesTable,
            {
              'id': message.id,
              'chat_id': message.chatId,
              'sender_id': message.senderId,
              'encrypted_content': encrypted['encrypted_content'],
              'nonce': encrypted['nonce'],
              'content_type': message.contentType,
              'created_at': message.createdAt.millisecondsSinceEpoch,
              'edited_at': message.editedAt?.millisecondsSinceEpoch,
              'status': message.status.name,
            },
            conflictAlgorithm: ConflictAlgorithm.replace,
          );
        }

        await batch.commit(noResult: true);
      });

      _logger.d('${messages.length} messages saved to cache');
    } catch (e) {
      _logger.e('Failed to save messages to cache', error: e);
      throw CacheException('Failed to save messages to cache', e);
    }
  }

  /// Get messages for a chat from the encrypted cache
  /// Messages are decrypted before returning
  Future<List<Message>> getMessages(
    String chatId, {
    int? limit,
    int? offset,
    DateTime? before,
    DateTime? after,
  }) async {
    _ensureInitialized();

    try {
      String whereClause = 'chat_id = ?';
      List<dynamic> whereArgs = [chatId];

      if (before != null) {
        whereClause += ' AND created_at < ?';
        whereArgs.add(before.millisecondsSinceEpoch);
      }

      if (after != null) {
        whereClause += ' AND created_at > ?';
        whereArgs.add(after.millisecondsSinceEpoch);
      }

      final results = await _database!.query(
        _messagesTable,
        where: whereClause,
        whereArgs: whereArgs,
        orderBy: 'created_at DESC',
        limit: limit,
        offset: offset,
      );

      final messages = <Message>[];
      for (final row in results) {
        try {
          // Decrypt the message content
          final decryptedContent = await _crypto.decryptFromCache(
            row['encrypted_content'] as String,
            row['nonce'] as String,
          );

          messages.add(Message(
            id: row['id'] as String,
            chatId: row['chat_id'] as String,
            senderId: row['sender_id'] as String,
            content: decryptedContent,
            contentType: row['content_type'] as String,
            createdAt: DateTime.fromMillisecondsSinceEpoch(row['created_at'] as int),
            editedAt: row['edited_at'] != null
                ? DateTime.fromMillisecondsSinceEpoch(row['edited_at'] as int)
                : null,
            status: MessageStatus.values.firstWhere(
              (s) => s.name == row['status'],
              orElse: () => MessageStatus.sent,
            ),
          ));
        } catch (e) {
          _logger.w('Failed to decrypt message ${row['id']}, skipping', error: e);
          // Skip messages that fail to decrypt (e.g., from different device key)
        }
      }

      _logger.d('Retrieved ${messages.length} messages from cache for chat $chatId');
      return messages;
    } catch (e) {
      _logger.e('Failed to get messages from cache', error: e);
      throw CacheException('Failed to get messages from cache', e);
    }
  }

  /// Get a single message by ID
  Future<Message?> getMessage(String messageId) async {
    _ensureInitialized();

    try {
      final results = await _database!.query(
        _messagesTable,
        where: 'id = ?',
        whereArgs: [messageId],
        limit: 1,
      );

      if (results.isEmpty) return null;

      final row = results.first;
      final decryptedContent = await _crypto.decryptFromCache(
        row['encrypted_content'] as String,
        row['nonce'] as String,
      );

      return Message(
        id: row['id'] as String,
        chatId: row['chat_id'] as String,
        senderId: row['sender_id'] as String,
        content: decryptedContent,
        contentType: row['content_type'] as String,
        createdAt: DateTime.fromMillisecondsSinceEpoch(row['created_at'] as int),
        editedAt: row['edited_at'] != null
            ? DateTime.fromMillisecondsSinceEpoch(row['edited_at'] as int)
            : null,
        status: MessageStatus.values.firstWhere(
          (s) => s.name == row['status'],
          orElse: () => MessageStatus.sent,
        ),
      );
    } catch (e) {
      _logger.e('Failed to get message from cache', error: e);
      throw CacheException('Failed to get message from cache', e);
    }
  }

  /// Delete a message from the cache
  Future<void> deleteMessage(String messageId) async {
    _ensureInitialized();

    try {
      await _database!.delete(
        _messagesTable,
        where: 'id = ?',
        whereArgs: [messageId],
      );
      _logger.d('Message $messageId deleted from cache');
    } catch (e) {
      _logger.e('Failed to delete message from cache', error: e);
      throw CacheException('Failed to delete message from cache', e);
    }
  }

  /// Clear all messages for a specific chat
  Future<void> clearChatCache(String chatId) async {
    _ensureInitialized();

    try {
      final count = await _database!.delete(
        _messagesTable,
        where: 'chat_id = ?',
        whereArgs: [chatId],
      );
      _logger.i('Cleared $count messages from cache for chat $chatId');
    } catch (e) {
      _logger.e('Failed to clear chat cache', error: e);
      throw CacheException('Failed to clear chat cache', e);
    }
  }

  /// Clear all cached data
  /// This is a destructive operation that removes all cached messages
  Future<void> clearCache() async {
    _ensureInitialized();

    try {
      await _database!.delete(_messagesTable);
      _logger.i('All cache data cleared');
    } catch (e) {
      _logger.e('Failed to clear cache', error: e);
      throw CacheException('Failed to clear cache', e);
    }
  }

  /// Get cache statistics
  Future<Map<String, dynamic>> getCacheStats() async {
    _ensureInitialized();

    try {
      final totalMessages = Sqflite.firstIntValue(
        await _database!.rawQuery('SELECT COUNT(*) FROM $_messagesTable'),
      ) ?? 0;

      final chatCount = Sqflite.firstIntValue(
        await _database!.rawQuery('SELECT COUNT(DISTINCT chat_id) FROM $_messagesTable'),
      ) ?? 0;

      final oldestMessage = await _database!.rawQuery(
        'SELECT MIN(created_at) as oldest FROM $_messagesTable',
      );

      final newestMessage = await _database!.rawQuery(
        'SELECT MAX(created_at) as newest FROM $_messagesTable',
      );

      return {
        'total_messages': totalMessages,
        'chat_count': chatCount,
        'oldest_message': oldestMessage.first['oldest'] != null
            ? DateTime.fromMillisecondsSinceEpoch(oldestMessage.first['oldest'] as int)
            : null,
        'newest_message': newestMessage.first['newest'] != null
            ? DateTime.fromMillisecondsSinceEpoch(newestMessage.first['newest'] as int)
            : null,
      };
    } catch (e) {
      _logger.e('Failed to get cache stats', error: e);
      throw CacheException('Failed to get cache stats', e);
    }
  }

  /// Check if a message exists in cache
  Future<bool> hasMessage(String messageId) async {
    _ensureInitialized();

    try {
      final count = Sqflite.firstIntValue(
        await _database!.rawQuery(
          'SELECT COUNT(*) FROM $_messagesTable WHERE id = ?',
          [messageId],
        ),
      );
      return (count ?? 0) > 0;
    } catch (e) {
      _logger.e('Failed to check message existence', error: e);
      return false;
    }
  }

  /// Close the database connection
  Future<void> close() async {
    if (_database != null) {
      await _database!.close();
      _database = null;
      _isInitialized = false;
      _logger.i('Cache service closed');
    }
  }

  /// Delete the cache database file completely
  /// Use this for complete data wipe (e.g., on logout)
  Future<void> deleteDatabase() async {
    try {
      if (_database != null) {
        await _database!.close();
        _database = null;
      }

      final documentsDirectory = await getApplicationDocumentsDirectory();
      final dbPath = join(documentsDirectory.path, _dbName);

      await databaseFactory.deleteDatabase(dbPath);
      _isInitialized = false;
      _logger.i('Cache database deleted');
    } catch (e) {
      _logger.e('Failed to delete cache database', error: e);
      throw CacheException('Failed to delete cache database', e);
    }
  }

  bool get isInitialized => _isInitialized;
}
