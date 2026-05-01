import 'dart:convert';
import 'dart:typed_data';
import 'package:cryptography/cryptography.dart';
import 'package:logger/logger.dart';

/// Exception thrown when cryptographic operations fail
class CryptoException implements Exception {
  final String message;
  final dynamic originalError;

  CryptoException(this.message, [this.originalError]);

  @override
  String toString() => 'CryptoException: $message';
}

/// Encrypted key bundle containing all data needed for decryption
class EncryptedKeyBundle {
  final String encryptedKey;
  final String nonce;
  final String salt;

  EncryptedKeyBundle({
    required this.encryptedKey,
    required this.nonce,
    required this.salt,
  });

  Map<String, String> toJson() => {
    'encrypted_key': encryptedKey,
    'nonce': nonce,
    'salt': salt,
  };

  factory EncryptedKeyBundle.fromJson(Map<String, dynamic> json) {
    return EncryptedKeyBundle(
      encryptedKey: json['encrypted_key'] as String,
      nonce: json['nonce'] as String,
      salt: json['salt'] as String,
    );
  }

  String toBase64() => base64Encode(utf8.encode(jsonEncode(toJson())));

  factory EncryptedKeyBundle.fromBase64(String encoded) {
    final json = jsonDecode(utf8.decode(base64Decode(encoded)));
    return EncryptedKeyBundle.fromJson(json);
  }
}

class CryptoService {
  final _logger = Logger();
  final _x25519 = X25519();
  final _xchacha20 = Xchacha20.poly1305Aead();
  // Use 32-byte hash to match Go's blake2b.Sum256
  final _blake2b = Blake2b(hashLengthInBytes: 32);

  // Argon2id parameters for passphrase-based key derivation
  // Using recommended parameters for interactive operations
  static const _argon2Memory = 64 * 1024; // 64 MB
  static const _argon2Iterations = 3;
  static const _argon2Parallelism = 1;
  static const _argon2HashLength = 32; // 256-bit key
  static const _saltLength = 16; // 128-bit salt

  // Key pair storage
  SimpleKeyPair? _deviceKeyPair;
  SimplePublicKey? _serverPublicKey;
  SecretKey? _sharedSecret;

  // Cache key for message encryption (derived from device key)
  SecretKey? _cacheKey;

  // Generate a new X25519 key pair
  Future<SimpleKeyPair> generateKeyPair() async {
    return await _x25519.newKeyPair();
  }

  // Set device key pair
  Future<void> setDeviceKeyPair(SimpleKeyPair keyPair) async {
    _deviceKeyPair = keyPair;
    if (_serverPublicKey != null) {
      await _computeSharedSecret();
    }
    // Derive cache key from device private key
    await _deriveCacheKey();
  }

  // Derive a cache key from the device private key for local message encryption
  Future<void> _deriveCacheKey() async {
    if (_deviceKeyPair == null) return;

    final privateKeyBytes = await _deviceKeyPair!.extractPrivateKeyBytes();
    // Use BLAKE2b to derive a separate key for cache encryption
    // This ensures cache key is deterministic but separate from communication keys
    final cacheKeyInput = [...privateKeyBytes, ...utf8.encode('cache_encryption_key_v1')];
    final hash = await _blake2b.hash(cacheKeyInput);
    _cacheKey = SecretKey(hash.bytes);
    _logger.d('Cache key derived from device key');
  }

  // Get the cache key for message cache encryption
  SecretKey? get cacheKey => _cacheKey;

  // Set server public key and compute shared secret
  Future<void> setServerPublicKey(List<int> publicKeyBytes) async {
    _serverPublicKey = SimplePublicKey(publicKeyBytes, type: KeyPairType.x25519);
    if (_deviceKeyPair != null) {
      await _computeSharedSecret();
    }
  }

  // Compute shared secret using X25519 + BLAKE2b
  Future<void> _computeSharedSecret() async {
    if (_deviceKeyPair == null || _serverPublicKey == null) return;

    final sharedSecretKey = await _x25519.sharedSecretKey(
      keyPair: _deviceKeyPair!,
      remotePublicKey: _serverPublicKey!,
    );

    // Derive encryption key using BLAKE2b
    final sharedBytes = await sharedSecretKey.extractBytes();
    final hash = await _blake2b.hash(sharedBytes);
    _sharedSecret = SecretKey(hash.bytes);

    _logger.d('Shared secret computed');
  }

  // Encrypt message content
  Future<Map<String, String>> encrypt(String plaintext) async {
    if (_sharedSecret == null) {
      throw CryptoException('Shared secret not computed. Set keys first.');
    }

    final plaintextBytes = utf8.encode(plaintext);

    // Generate random nonce (24 bytes for XChaCha20)
    final nonce = _xchacha20.newNonce();

    final secretBox = await _xchacha20.encrypt(
      plaintextBytes,
      secretKey: _sharedSecret!,
      nonce: nonce,
    );

    // Concatenate ciphertext + MAC (Go's format: aead.Seal returns ciphertext || tag)
    final ciphertextWithMac = [...secretBox.cipherText, ...secretBox.mac.bytes];

    return {
      'encrypted_content': base64Encode(ciphertextWithMac),
      'nonce': base64Encode(nonce),
    };
  }

  // Decrypt message content
  Future<String> decrypt(String encryptedContent, String nonceBase64) async {
    _logger.d('Decrypt: encryptedContent length=${encryptedContent.length}, nonce length=${nonceBase64.length}');

    if (_sharedSecret == null) {
      _logger.e('Decrypt: Shared secret is null!');
      throw CryptoException('Shared secret not computed. Set keys first.');
    }

    final ciphertext = base64Decode(encryptedContent);
    final nonce = base64Decode(nonceBase64);

    _logger.d('Decrypt: ciphertext bytes=${ciphertext.length}, nonce bytes=${nonce.length}');

    // XChaCha20-Poly1305 includes MAC in ciphertext (last 16 bytes)
    final macBytes = ciphertext.sublist(ciphertext.length - 16);
    final actualCiphertext = ciphertext.sublist(0, ciphertext.length - 16);

    _logger.d('Decrypt: actualCiphertext=${actualCiphertext.length}, mac=${macBytes.length}');

    final secretBox = SecretBox(
      actualCiphertext,
      nonce: nonce,
      mac: Mac(macBytes),
    );

    try {
      final plaintext = await _xchacha20.decrypt(
        secretBox,
        secretKey: _sharedSecret!,
      );
      _logger.d('Decrypt: success, plaintext length=${plaintext.length}');
      return utf8.decode(plaintext);
    } catch (e) {
      _logger.e('Decrypt: failed with error: $e');
      rethrow;
    }
  }

  // ============ Cache Encryption (for local message storage) ============

  /// Encrypt data for local cache storage using the cache key
  Future<Map<String, String>> encryptForCache(String plaintext) async {
    if (_cacheKey == null) {
      throw CryptoException('Cache key not initialized. Set device key pair first.');
    }

    final plaintextBytes = utf8.encode(plaintext);
    final nonce = _xchacha20.newNonce();

    final secretBox = await _xchacha20.encrypt(
      plaintextBytes,
      secretKey: _cacheKey!,
      nonce: nonce,
    );

    final ciphertextWithMac = [...secretBox.cipherText, ...secretBox.mac.bytes];

    return {
      'encrypted_content': base64Encode(ciphertextWithMac),
      'nonce': base64Encode(nonce),
    };
  }

  /// Decrypt data from local cache storage using the cache key
  Future<String> decryptFromCache(String encryptedContent, String nonceBase64) async {
    if (_cacheKey == null) {
      throw CryptoException('Cache key not initialized. Set device key pair first.');
    }

    final ciphertext = base64Decode(encryptedContent);
    final nonce = base64Decode(nonceBase64);

    final macBytes = ciphertext.sublist(ciphertext.length - 16);
    final actualCiphertext = ciphertext.sublist(0, ciphertext.length - 16);

    final secretBox = SecretBox(
      actualCiphertext,
      nonce: nonce,
      mac: Mac(macBytes),
    );

    try {
      final plaintext = await _xchacha20.decrypt(
        secretBox,
        secretKey: _cacheKey!,
      );
      return utf8.decode(plaintext);
    } catch (e) {
      _logger.e('Cache decrypt failed: $e');
      throw CryptoException('Failed to decrypt cached data', e);
    }
  }

  // ============ Key Export/Import with Passphrase Encryption ============

  /// Derive an encryption key from a passphrase using Argon2id
  Future<SecretKey> _deriveKeyFromPassphrase(String passphrase, List<int> salt) async {
    final argon2 = Argon2id(
      memory: _argon2Memory,
      iterations: _argon2Iterations,
      parallelism: _argon2Parallelism,
      hashLength: _argon2HashLength,
    );

    final derivedKey = await argon2.deriveKey(
      secretKey: SecretKey(utf8.encode(passphrase)),
      nonce: salt,
    );

    return derivedKey;
  }

  /// Generate a random salt for key derivation
  List<int> _generateSalt() {
    // Use XChaCha20's nonce generator for randomness (24 bytes, we take first 16)
    final randomBytes = _xchacha20.newNonce();
    return randomBytes.sublist(0, _saltLength);
  }

  /// Export private key encrypted with a passphrase
  /// Returns an EncryptedKeyBundle containing the encrypted key, nonce, and salt
  Future<EncryptedKeyBundle> exportPrivateKeyEncrypted(String passphrase) async {
    if (_deviceKeyPair == null) {
      throw CryptoException('No key pair generated');
    }

    if (passphrase.isEmpty) {
      throw CryptoException('Passphrase cannot be empty');
    }

    // Enforce minimum passphrase strength
    if (passphrase.length < 8) {
      throw CryptoException('Passphrase must be at least 8 characters');
    }

    // Generate salt and derive key
    final salt = _generateSalt();
    final derivedKey = await _deriveKeyFromPassphrase(passphrase, salt);

    // Get private key bytes
    final privateKeyBytes = await _deviceKeyPair!.extractPrivateKeyBytes();

    // Encrypt with XChaCha20-Poly1305
    final nonce = _xchacha20.newNonce();
    final secretBox = await _xchacha20.encrypt(
      privateKeyBytes,
      secretKey: derivedKey,
      nonce: nonce,
    );

    // Combine ciphertext and MAC
    final ciphertextWithMac = [...secretBox.cipherText, ...secretBox.mac.bytes];

    _logger.i('Private key exported with passphrase encryption');

    return EncryptedKeyBundle(
      encryptedKey: base64Encode(ciphertextWithMac),
      nonce: base64Encode(nonce),
      salt: base64Encode(salt),
    );
  }

  /// Import private key from an encrypted bundle using the passphrase
  /// Also requires the public key for reconstructing the key pair
  Future<void> importPrivateKeyEncrypted(
    EncryptedKeyBundle bundle,
    String publicKeyBase64,
    String passphrase,
  ) async {
    if (passphrase.isEmpty) {
      throw CryptoException('Passphrase cannot be empty');
    }

    // Decode components
    final salt = base64Decode(bundle.salt);
    final nonce = base64Decode(bundle.nonce);
    final ciphertext = base64Decode(bundle.encryptedKey);

    // Derive key from passphrase
    final derivedKey = await _deriveKeyFromPassphrase(passphrase, salt);

    // Extract MAC and ciphertext
    final macBytes = ciphertext.sublist(ciphertext.length - 16);
    final actualCiphertext = ciphertext.sublist(0, ciphertext.length - 16);

    final secretBox = SecretBox(
      actualCiphertext,
      nonce: nonce,
      mac: Mac(macBytes),
    );

    // Decrypt private key
    List<int> privateKeyBytes;
    try {
      privateKeyBytes = await _xchacha20.decrypt(
        secretBox,
        secretKey: derivedKey,
      );
    } catch (e) {
      _logger.e('Failed to decrypt private key - likely wrong passphrase');
      throw CryptoException(
        'Failed to decrypt private key. Please check your passphrase.',
        e,
      );
    }

    // Reconstruct key pair
    final publicKeyBytes = base64Decode(publicKeyBase64);
    _deviceKeyPair = SimpleKeyPairData(
      privateKeyBytes,
      publicKey: SimplePublicKey(publicKeyBytes, type: KeyPairType.x25519),
      type: KeyPairType.x25519,
    );

    // Derive cache key from restored key
    await _deriveCacheKey();

    if (_serverPublicKey != null) {
      await _computeSharedSecret();
    }

    _logger.i('Private key imported from encrypted bundle');
  }

  // Export public key as base64
  Future<String> exportPublicKey() async {
    if (_deviceKeyPair == null) {
      throw CryptoException('No key pair generated');
    }
    final publicKey = await _deviceKeyPair!.extractPublicKey();
    return base64Encode(publicKey.bytes);
  }

  // Export private key as base64 (for secure storage - use encrypted export for backup)
  Future<String> exportPrivateKey() async {
    if (_deviceKeyPair == null) {
      throw CryptoException('No key pair generated');
    }
    final privateKey = await _deviceKeyPair!.extractPrivateKeyBytes();
    return base64Encode(privateKey);
  }

  // Import key pair from stored bytes
  Future<void> importKeyPair(String privateKeyBase64, String publicKeyBase64) async {
    final privateKeyBytes = base64Decode(privateKeyBase64);
    final publicKeyBytes = base64Decode(publicKeyBase64);

    _deviceKeyPair = SimpleKeyPairData(
      privateKeyBytes,
      publicKey: SimplePublicKey(publicKeyBytes, type: KeyPairType.x25519),
      type: KeyPairType.x25519,
    );

    // Derive cache key from restored key
    await _deriveCacheKey();

    if (_serverPublicKey != null) {
      await _computeSharedSecret();
    }
  }

  // Compute key fingerprint for verification
  Future<String> computeFingerprint(List<int> publicKeyBytes) async {
    final hash = await _blake2b.hash(publicKeyBytes);
    final hex = hash.bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    // Format as groups of 4 for readability
    final buffer = StringBuffer();
    for (int i = 0; i < hex.length; i += 4) {
      if (i > 0) buffer.write(' ');
      buffer.write(hex.substring(i, i + 4 > hex.length ? hex.length : i + 4));
    }
    return buffer.toString().toUpperCase();
  }

  bool get isInitialized => _sharedSecret != null;
  bool get hasCacheKey => _cacheKey != null;
}
