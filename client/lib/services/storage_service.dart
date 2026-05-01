import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:logger/logger.dart';

/// Exception thrown when secure storage operations fail
class SecureStorageException implements Exception {
  final String message;
  final dynamic originalError;

  SecureStorageException(this.message, [this.originalError]);

  @override
  String toString() => 'SecureStorageException: $message';
}

class StorageService {
  final _logger = Logger();
  final _secureStorage = const FlutterSecureStorage(
    aOptions: AndroidOptions(encryptedSharedPreferences: true),
    iOptions: IOSOptions(accessibility: KeychainAccessibility.first_unlock),
  );

  SharedPreferences? _prefs;

  // Keys
  static const _keyToken = 'auth_token';
  static const _keyUserId = 'user_id';
  static const _keyUsername = 'username';
  static const _keyEmail = 'email';
  static const _keyDeviceId = 'device_id';
  static const _keyPrivateKey = 'device_private_key';
  static const _keyPublicKey = 'device_public_key';
  static const _keyServerPublicKey = 'server_public_key';
  static const _keyServerUrl = 'server_url';

  Future<void> init() async {
    _prefs = await SharedPreferences.getInstance();
  }

  // ============ Secure Storage (sensitive data) ============
  // Secure storage is required - no plaintext fallback for security

  Future<void> _secureWrite(String key, String value) async {
    try {
      await _secureStorage.write(key: key, value: value);
    } catch (e) {
      _logger.e('Secure storage write failed for key: $key', error: e);
      throw SecureStorageException(
        'Failed to write to secure storage. '
        'Please ensure your device has a secure keychain/keystore available. '
        'On Linux, you may need to install and configure a keyring service (e.g., gnome-keyring).',
        e,
      );
    }
  }

  Future<String?> _secureRead(String key) async {
    try {
      return await _secureStorage.read(key: key);
    } catch (e) {
      _logger.e('Secure storage read failed for key: $key', error: e);
      throw SecureStorageException(
        'Failed to read from secure storage. '
        'Please ensure your device has a secure keychain/keystore available. '
        'On Linux, you may need to install and configure a keyring service (e.g., gnome-keyring).',
        e,
      );
    }
  }

  Future<void> _secureDelete(String key) async {
    try {
      await _secureStorage.delete(key: key);
    } catch (e) {
      _logger.e('Secure storage delete failed for key: $key', error: e);
      throw SecureStorageException(
        'Failed to delete from secure storage.',
        e,
      );
    }
  }

  Future<void> saveToken(String token) async {
    await _secureWrite(_keyToken, token);
  }

  Future<String?> getToken() async {
    return await _secureRead(_keyToken);
  }

  Future<void> savePrivateKey(String privateKey) async {
    await _secureWrite(_keyPrivateKey, privateKey);
  }

  Future<String?> getPrivateKey() async {
    return await _secureRead(_keyPrivateKey);
  }

  Future<void> savePublicKey(String publicKey) async {
    await _secureWrite(_keyPublicKey, publicKey);
  }

  Future<String?> getPublicKey() async {
    return await _secureRead(_keyPublicKey);
  }

  Future<void> saveServerPublicKey(String serverPublicKey) async {
    await _secureWrite(_keyServerPublicKey, serverPublicKey);
  }

  Future<String?> getServerPublicKey() async {
    return await _secureRead(_keyServerPublicKey);
  }

  Future<void> clearSecureStorage() async {
    try {
      await _secureStorage.deleteAll();
      _logger.i('Secure storage cleared');
    } catch (e) {
      _logger.e('Failed to clear secure storage', error: e);
      throw SecureStorageException(
        'Failed to clear secure storage.',
        e,
      );
    }
  }

  /// Check if secure storage is available and working
  Future<bool> isSecureStorageAvailable() async {
    try {
      const testKey = '_secure_storage_test';
      const testValue = 'test';
      await _secureStorage.write(key: testKey, value: testValue);
      final result = await _secureStorage.read(key: testKey);
      await _secureStorage.delete(key: testKey);
      return result == testValue;
    } catch (e) {
      _logger.w('Secure storage availability check failed', error: e);
      return false;
    }
  }

  // ============ Shared Preferences (non-sensitive data) ============

  Future<void> saveUserId(String userId) async {
    await _prefs?.setString(_keyUserId, userId);
  }

  String? getUserId() => _prefs?.getString(_keyUserId);

  Future<void> saveUsername(String username) async {
    await _prefs?.setString(_keyUsername, username);
  }

  String? getUsername() => _prefs?.getString(_keyUsername);

  Future<void> saveEmail(String email) async {
    await _prefs?.setString(_keyEmail, email);
  }

  String? getEmail() => _prefs?.getString(_keyEmail);

  Future<void> saveDeviceId(String deviceId) async {
    await _prefs?.setString(_keyDeviceId, deviceId);
  }

  String? getDeviceId() => _prefs?.getString(_keyDeviceId);

  Future<void> saveServerUrl(String url) async {
    await _prefs?.setString(_keyServerUrl, url);
  }

  String getServerUrl() => _prefs?.getString(_keyServerUrl) ?? 'http://localhost:8080';

  // ============ User Session ============

  Future<void> saveUserSession({
    required String token,
    required String userId,
    required String username,
    required String email,
    required String deviceId,
    required String privateKey,
    required String publicKey,
    required String serverPublicKey,
  }) async {
    // First check if secure storage is available
    if (!await isSecureStorageAvailable()) {
      throw SecureStorageException(
        'Secure storage is not available on this device. '
        'Cannot save session securely. '
        'Please ensure your device has a secure keychain/keystore configured.',
      );
    }

    await Future.wait([
      saveToken(token),
      saveUserId(userId),
      saveUsername(username),
      saveEmail(email),
      saveDeviceId(deviceId),
      savePrivateKey(privateKey),
      savePublicKey(publicKey),
      saveServerPublicKey(serverPublicKey),
    ]);
    _logger.i('User session saved');
  }

  Future<Map<String, String?>> getUserSession() async {
    return {
      'token': await getToken(),
      'userId': getUserId(),
      'username': getUsername(),
      'email': getEmail(),
      'deviceId': getDeviceId(),
      'privateKey': await getPrivateKey(),
      'publicKey': await getPublicKey(),
      'serverPublicKey': await getServerPublicKey(),
    };
  }

  Future<bool> hasSession() async {
    try {
      final token = await getToken();
      return token != null && token.isNotEmpty;
    } catch (e) {
      // If secure storage fails during check, treat as no session
      _logger.w('Failed to check session status', error: e);
      return false;
    }
  }

  Future<void> clearSession() async {
    await clearSecureStorage();
    await _prefs?.remove(_keyUserId);
    await _prefs?.remove(_keyUsername);
    await _prefs?.remove(_keyEmail);
    await _prefs?.remove(_keyDeviceId);
    _logger.i('Session cleared');
  }
}
