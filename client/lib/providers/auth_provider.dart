import 'dart:convert';
import 'package:flutter/foundation.dart';
import '../services/services.dart';

enum AuthState { initial, loading, authenticated, unauthenticated, error }

/// Error types for authentication failures
enum AuthErrorType {
  unknown,
  secureStorageUnavailable,
  networkError,
  invalidCredentials,
  serverError,
  keyExportFailed,
  keyImportFailed,
}

/// Detailed auth error with type and message
class AuthError {
  final AuthErrorType type;
  final String message;
  final String? userFriendlyMessage;

  AuthError({
    required this.type,
    required this.message,
    this.userFriendlyMessage,
  });

  @override
  String toString() => userFriendlyMessage ?? message;
}

class AuthProvider extends ChangeNotifier {
  final ApiService _api;
  final CryptoService _crypto;
  final StorageService _storage;
  final WebSocketService _ws;
  final DeviceInfoService _deviceInfo;
  final CacheService? _cache;

  AuthState _state = AuthState.initial;
  String? _userId;
  String? _username;
  String? _email;
  AuthError? _error;

  AuthProvider({
    required ApiService api,
    required CryptoService crypto,
    required StorageService storage,
    required WebSocketService ws,
    DeviceInfoService? deviceInfo,
    CacheService? cache,
  })  : _api = api,
        _crypto = crypto,
        _storage = storage,
        _ws = ws,
        _deviceInfo = deviceInfo ?? DeviceInfoService(),
        _cache = cache;

  AuthState get state => _state;
  String? get userId => _userId;
  String? get username => _username;
  String? get email => _email;
  AuthError? get error => _error;
  String? get errorMessage => _error?.toString();
  bool get isAuthenticated => _state == AuthState.authenticated;

  /// Check if secure storage is available
  Future<bool> isSecureStorageAvailable() async {
    return await _storage.isSecureStorageAvailable();
  }

  Future<void> init() async {
    await _storage.init();

    // Check secure storage availability first
    if (!await _storage.isSecureStorageAvailable()) {
      _state = AuthState.error;
      _error = AuthError(
        type: AuthErrorType.secureStorageUnavailable,
        message: 'Secure storage is not available',
        userFriendlyMessage:
            'Secure storage is not available on this device. '
            'Please ensure your device has a secure keychain/keystore configured. '
            'On Linux, you may need to install and configure gnome-keyring or another keyring service.',
      );
      notifyListeners();
      return;
    }

    if (await _storage.hasSession()) {
      await _restoreSession();
    } else {
      _state = AuthState.unauthenticated;
      notifyListeners();
    }
  }

  Future<void> _restoreSession() async {
    _state = AuthState.loading;
    notifyListeners();

    try {
      final session = await _storage.getUserSession();

      if (session['token'] == null || session['privateKey'] == null) {
        _state = AuthState.unauthenticated;
        notifyListeners();
        return;
      }

      // Restore crypto keys
      await _crypto.importKeyPair(
        session['privateKey']!,
        session['publicKey']!,
      );

      if (session['serverPublicKey'] != null) {
        await _crypto.setServerPublicKey(
          base64Decode(session['serverPublicKey']!),
        );
      }

      // Initialize cache if available
      if (_cache != null && _crypto.hasCacheKey) {
        try {
          await _cache!.init();
        } catch (e) {
          // Cache initialization failure is not fatal
          debugPrint('Cache initialization failed: $e');
        }
      }

      // Set API token
      _api.setToken(session['token']!);

      // Connect WebSocket
      _ws.setToken(session['token']!);
      await _ws.connect();

      _userId = session['userId'];
      _username = session['username'];
      _email = session['email'];
      _state = AuthState.authenticated;
      _error = null;
    } on SecureStorageException catch (e) {
      _error = AuthError(
        type: AuthErrorType.secureStorageUnavailable,
        message: e.message,
        userFriendlyMessage:
            'Failed to access secure storage. '
            'Please ensure your device has proper security settings configured.',
      );
      _state = AuthState.error;
    } catch (e) {
      _error = AuthError(
        type: AuthErrorType.unknown,
        message: e.toString(),
      );
      _state = AuthState.unauthenticated;
    }

    notifyListeners();
  }

  Future<bool> register({
    required String username,
    required String email,
    required String password,
  }) async {
    _state = AuthState.loading;
    _error = null;
    notifyListeners();

    try {
      // Check secure storage first
      if (!await _storage.isSecureStorageAvailable()) {
        _error = AuthError(
          type: AuthErrorType.secureStorageUnavailable,
          message: 'Secure storage unavailable',
          userFriendlyMessage:
              'Cannot register: secure storage is not available. '
              'Please configure a secure keychain/keystore on your device.',
        );
        _state = AuthState.error;
        notifyListeners();
        return false;
      }

      // Generate device key pair
      final keyPair = await _crypto.generateKeyPair();
      await _crypto.setDeviceKeyPair(keyPair);

      final publicKey = await _crypto.exportPublicKey();
      final privateKey = await _crypto.exportPrivateKey();

      // Collect device profile
      final deviceProfile = await _deviceInfo.getDeviceProfile();
      final deviceName = await _deviceInfo.getDeviceName();

      // Register with server
      final result = await _api.register(
        username: username,
        email: email,
        password: password,
        publicKey: publicKey,
        deviceName: deviceName,
        deviceKey: publicKey,
        deviceProfile: deviceProfile.toJson(),
      );

      // Extract response data
      final token = result['token'] as String;
      final user = result['user'] as Map<String, dynamic>;
      final device = result['device'] as Map<String, dynamic>;
      final serverPublicKey = result['server_public_key'] as String;

      // Set server public key for encryption
      await _crypto.setServerPublicKey(base64Decode(serverPublicKey));

      // Save session
      await _storage.saveUserSession(
        token: token,
        userId: user['id'],
        username: user['username'],
        email: email,
        deviceId: device['id'],
        privateKey: privateKey,
        publicKey: publicKey,
        serverPublicKey: serverPublicKey,
      );

      // Initialize cache
      if (_cache != null && _crypto.hasCacheKey) {
        try {
          await _cache!.init();
        } catch (e) {
          debugPrint('Cache initialization failed: $e');
        }
      }

      // Connect WebSocket
      _ws.setToken(token);
      await _ws.connect();

      _userId = user['id'];
      _username = user['username'];
      _email = email;
      _state = AuthState.authenticated;
      _error = null;
      notifyListeners();

      return true;
    } on SecureStorageException catch (e) {
      _error = AuthError(
        type: AuthErrorType.secureStorageUnavailable,
        message: e.message,
        userFriendlyMessage:
            'Failed to save credentials securely. '
            'Please ensure secure storage is available on your device.',
      );
      _state = AuthState.error;
      notifyListeners();
      return false;
    } catch (e) {
      _error = AuthError(
        type: AuthErrorType.unknown,
        message: e.toString(),
      );
      _state = AuthState.error;
      notifyListeners();
      return false;
    }
  }

  Future<bool> login({
    required String email,
    required String password,
  }) async {
    _state = AuthState.loading;
    _error = null;
    notifyListeners();

    try {
      // Check secure storage first
      if (!await _storage.isSecureStorageAvailable()) {
        _error = AuthError(
          type: AuthErrorType.secureStorageUnavailable,
          message: 'Secure storage unavailable',
          userFriendlyMessage:
              'Cannot login: secure storage is not available. '
              'Please configure a secure keychain/keystore on your device.',
        );
        _state = AuthState.error;
        notifyListeners();
        return false;
      }

      // Generate device key pair
      final keyPair = await _crypto.generateKeyPair();
      await _crypto.setDeviceKeyPair(keyPair);

      final publicKey = await _crypto.exportPublicKey();
      final privateKey = await _crypto.exportPrivateKey();

      // Collect device profile
      final deviceProfile = await _deviceInfo.getDeviceProfile();
      final deviceName = await _deviceInfo.getDeviceName();

      // Login with server
      final result = await _api.login(
        email: email,
        password: password,
        deviceName: deviceName,
        deviceKey: publicKey,
        deviceProfile: deviceProfile.toJson(),
      );

      // Extract response data
      final token = result['token'] as String;
      final user = result['user'] as Map<String, dynamic>;
      final device = result['device'] as Map<String, dynamic>;
      final serverPublicKey = result['server_public_key'] as String;

      // Set server public key for encryption
      await _crypto.setServerPublicKey(base64Decode(serverPublicKey));

      // Save session
      await _storage.saveUserSession(
        token: token,
        userId: user['id'],
        username: user['username'],
        email: email,
        deviceId: device['id'],
        privateKey: privateKey,
        publicKey: publicKey,
        serverPublicKey: serverPublicKey,
      );

      // Initialize cache
      if (_cache != null && _crypto.hasCacheKey) {
        try {
          await _cache!.init();
        } catch (e) {
          debugPrint('Cache initialization failed: $e');
        }
      }

      // Connect WebSocket
      _ws.setToken(token);
      await _ws.connect();

      _userId = user['id'];
      _username = user['username'];
      _email = email;
      _state = AuthState.authenticated;
      _error = null;
      notifyListeners();

      return true;
    } on SecureStorageException catch (e) {
      _error = AuthError(
        type: AuthErrorType.secureStorageUnavailable,
        message: e.message,
        userFriendlyMessage:
            'Failed to save credentials securely. '
            'Please ensure secure storage is available on your device.',
      );
      _state = AuthState.error;
      notifyListeners();
      return false;
    } catch (e) {
      _error = AuthError(
        type: AuthErrorType.unknown,
        message: e.toString(),
      );
      _state = AuthState.error;
      notifyListeners();
      return false;
    }
  }

  Future<void> logout() async {
    try {
      await _api.logout();
    } catch (_) {}

    _ws.disconnect();
    _api.clearToken();

    // Clear cache on logout
    if (_cache != null && _cache!.isInitialized) {
      try {
        await _cache!.deleteDatabase();
      } catch (e) {
        debugPrint('Failed to clear cache: $e');
      }
    }

    await _storage.clearSession();

    _userId = null;
    _username = null;
    _email = null;
    _error = null;
    _state = AuthState.unauthenticated;
    notifyListeners();
  }

  // ============ Encrypted Key Export/Import ============

  /// Export private key encrypted with a passphrase for backup
  /// Returns a base64-encoded bundle containing encrypted key, nonce, and salt
  Future<String> exportEncryptedPrivateKey(String passphrase) async {
    try {
      final bundle = await _crypto.exportPrivateKeyEncrypted(passphrase);
      return bundle.toBase64();
    } on CryptoException catch (e) {
      _error = AuthError(
        type: AuthErrorType.keyExportFailed,
        message: e.message,
        userFriendlyMessage: 'Failed to export private key: ${e.message}',
      );
      notifyListeners();
      rethrow;
    }
  }

  /// Get public key for backup (not encrypted, needed for key import)
  Future<String> getPublicKeyForBackup() async {
    return await _crypto.exportPublicKey();
  }

  /// Import private key from an encrypted backup
  /// Requires the encrypted bundle, public key, and passphrase
  Future<bool> importEncryptedPrivateKey({
    required String encryptedBundle,
    required String publicKey,
    required String passphrase,
  }) async {
    try {
      final bundle = EncryptedKeyBundle.fromBase64(encryptedBundle);
      await _crypto.importPrivateKeyEncrypted(bundle, publicKey, passphrase);

      // Save the restored keys
      final privateKey = await _crypto.exportPrivateKey();
      await _storage.savePrivateKey(privateKey);
      await _storage.savePublicKey(publicKey);

      // Reinitialize cache with new key
      if (_cache != null && _crypto.hasCacheKey) {
        try {
          await _cache!.init();
        } catch (e) {
          debugPrint('Cache initialization failed: $e');
        }
      }

      notifyListeners();
      return true;
    } on CryptoException catch (e) {
      _error = AuthError(
        type: AuthErrorType.keyImportFailed,
        message: e.message,
        userFriendlyMessage: 'Failed to import private key: ${e.message}',
      );
      notifyListeners();
      return false;
    } on SecureStorageException catch (e) {
      _error = AuthError(
        type: AuthErrorType.secureStorageUnavailable,
        message: e.message,
        userFriendlyMessage:
            'Failed to save imported key: secure storage unavailable.',
      );
      notifyListeners();
      return false;
    } catch (e) {
      _error = AuthError(
        type: AuthErrorType.unknown,
        message: e.toString(),
        userFriendlyMessage: 'Failed to import private key.',
      );
      notifyListeners();
      return false;
    }
  }

  /// Clear the current error
  void clearError() {
    _error = null;
    if (_state == AuthState.error) {
      _state = AuthState.unauthenticated;
    }
    notifyListeners();
  }
}
