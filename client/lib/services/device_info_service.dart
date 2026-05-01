import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:device_info_plus/device_info_plus.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:logger/logger.dart';

/// Represents detailed device profile information
class DeviceProfile {
  final String platform;
  final String osVersion;
  final String? manufacturer;
  final String? model;
  final String? hardwareId;
  final int? screenWidth;
  final int? screenHeight;
  final String? cpuArch;
  final int? memoryMb;
  final String appVersion;
  final String? bluetoothAddress;
  final String? wifiDirectAddress;
  final bool meshCapable;
  final String? connectionType;

  DeviceProfile({
    required this.platform,
    required this.osVersion,
    this.manufacturer,
    this.model,
    this.hardwareId,
    this.screenWidth,
    this.screenHeight,
    this.cpuArch,
    this.memoryMb,
    required this.appVersion,
    this.bluetoothAddress,
    this.wifiDirectAddress,
    this.meshCapable = false,
    this.connectionType,
  });

  Map<String, dynamic> toJson() => {
        'platform': platform,
        'os_version': osVersion,
        if (manufacturer != null) 'manufacturer': manufacturer,
        if (model != null) 'model': model,
        if (hardwareId != null) 'hardware_id': hardwareId,
        if (screenWidth != null) 'screen_width': screenWidth,
        if (screenHeight != null) 'screen_height': screenHeight,
        if (cpuArch != null) 'cpu_arch': cpuArch,
        if (memoryMb != null) 'memory_mb': memoryMb,
        'app_version': appVersion,
        if (bluetoothAddress != null) 'bluetooth_address': bluetoothAddress,
        if (wifiDirectAddress != null) 'wifi_direct_address': wifiDirectAddress,
        'mesh_capable': meshCapable,
        if (connectionType != null) 'connection_type': connectionType,
      };

  @override
  String toString() => 'DeviceProfile(platform: $platform, os: $osVersion, '
      'model: $model, appVersion: $appVersion)';
}

/// Service for collecting device information
class DeviceInfoService {
  final _logger = Logger();
  final _deviceInfo = DeviceInfoPlugin();

  DeviceProfile? _cachedProfile;

  /// Get the current device profile
  Future<DeviceProfile> getDeviceProfile() async {
    if (_cachedProfile != null) {
      // Update connection type which can change
      final connectionType = await _getConnectionType();
      return DeviceProfile(
        platform: _cachedProfile!.platform,
        osVersion: _cachedProfile!.osVersion,
        manufacturer: _cachedProfile!.manufacturer,
        model: _cachedProfile!.model,
        hardwareId: _cachedProfile!.hardwareId,
        screenWidth: _cachedProfile!.screenWidth,
        screenHeight: _cachedProfile!.screenHeight,
        cpuArch: _cachedProfile!.cpuArch,
        memoryMb: _cachedProfile!.memoryMb,
        appVersion: _cachedProfile!.appVersion,
        bluetoothAddress: _cachedProfile!.bluetoothAddress,
        wifiDirectAddress: _cachedProfile!.wifiDirectAddress,
        meshCapable: _cachedProfile!.meshCapable,
        connectionType: connectionType,
      );
    }

    try {
      final packageInfo = await PackageInfo.fromPlatform();
      final connectionType = await _getConnectionType();

      if (kIsWeb) {
        final webInfo = await _deviceInfo.webBrowserInfo;
        // Parse user agent for better OS identification
        final userAgent = webInfo.userAgent ?? '';
        String osFromUA = 'Unknown';
        if (userAgent.contains('Windows')) {
          osFromUA = 'Windows';
          final match = RegExp(r'Windows NT ([\d.]+)').firstMatch(userAgent);
          if (match != null) osFromUA = 'Windows ${match.group(1)}';
        } else if (userAgent.contains('Mac OS X')) {
          final match = RegExp(r'Mac OS X ([\d_]+)').firstMatch(userAgent);
          if (match != null) {
            osFromUA = 'macOS ${match.group(1)!.replaceAll('_', '.')}';
          } else {
            osFromUA = 'macOS';
          }
        } else if (userAgent.contains('Linux')) {
          osFromUA = 'Linux';
          if (userAgent.contains('Android')) {
            final match = RegExp(r'Android ([\d.]+)').firstMatch(userAgent);
            if (match != null) osFromUA = 'Android ${match.group(1)}';
          }
        } else if (userAgent.contains('iPhone') || userAgent.contains('iPad')) {
          final match = RegExp(r'OS ([\d_]+)').firstMatch(userAgent);
          if (match != null) {
            osFromUA = 'iOS ${match.group(1)!.replaceAll('_', '.')}';
          } else {
            osFromUA = 'iOS';
          }
        }

        _cachedProfile = DeviceProfile(
          platform: 'web',
          osVersion: '$osFromUA (${webInfo.browserName.name})',
          manufacturer: webInfo.vendor,
          model: webInfo.platform,
          hardwareId: null, // Not available in web
          appVersion: packageInfo.version,
          connectionType: connectionType,
          meshCapable: false, // Web doesn't support mesh networking
        );
      } else if (Platform.isAndroid) {
        final androidInfo = await _deviceInfo.androidInfo;
        _cachedProfile = DeviceProfile(
          platform: 'android',
          osVersion: 'Android ${androidInfo.version.release} (SDK ${androidInfo.version.sdkInt})',
          manufacturer: androidInfo.manufacturer,
          model: androidInfo.model,
          hardwareId: androidInfo.id,
          cpuArch: androidInfo.supportedAbis.isNotEmpty ? androidInfo.supportedAbis.first : null,
          appVersion: packageInfo.version,
          connectionType: connectionType,
          meshCapable: true, // Android supports WiFi Direct and Bluetooth
        );
      } else if (Platform.isIOS) {
        final iosInfo = await _deviceInfo.iosInfo;
        _cachedProfile = DeviceProfile(
          platform: 'ios',
          osVersion: 'iOS ${iosInfo.systemVersion}',
          manufacturer: 'Apple',
          model: iosInfo.model,
          hardwareId: iosInfo.identifierForVendor,
          appVersion: packageInfo.version,
          connectionType: connectionType,
          meshCapable: true, // iOS supports MultipeerConnectivity
        );
      } else if (Platform.isLinux) {
        final linuxInfo = await _deviceInfo.linuxInfo;
        // Build detailed Linux version string with kernel info
        String osVersion = linuxInfo.prettyName;
        if (linuxInfo.version != null && linuxInfo.version!.isNotEmpty) {
          osVersion += ' (${linuxInfo.version})';
        }
        if (linuxInfo.versionCodename != null && linuxInfo.versionCodename!.isNotEmpty) {
          osVersion += ' ${linuxInfo.versionCodename}';
        }

        // Get CPU architecture from environment or uname
        String? cpuArch;
        try {
          final result = await Process.run('uname', ['-m']);
          if (result.exitCode == 0) {
            cpuArch = result.stdout.toString().trim();
          }
        } catch (_) {}

        // Get memory info
        int? memoryMb;
        try {
          final memInfo = await File('/proc/meminfo').readAsString();
          final match = RegExp(r'MemTotal:\s+(\d+)\s+kB').firstMatch(memInfo);
          if (match != null) {
            memoryMb = int.parse(match.group(1)!) ~/ 1024;
          }
        } catch (_) {}

        _cachedProfile = DeviceProfile(
          platform: 'linux',
          osVersion: osVersion,
          manufacturer: linuxInfo.id, // Distribution ID (e.g., "arch", "ubuntu")
          model: linuxInfo.buildId ?? linuxInfo.variantId,
          hardwareId: linuxInfo.machineId,
          cpuArch: cpuArch,
          memoryMb: memoryMb,
          appVersion: packageInfo.version,
          connectionType: connectionType,
          meshCapable: true, // Linux can support Bluetooth/WiFi Direct
        );
      } else if (Platform.isMacOS) {
        final macInfo = await _deviceInfo.macOsInfo;
        _cachedProfile = DeviceProfile(
          platform: 'macos',
          osVersion: 'macOS ${macInfo.osRelease}',
          manufacturer: 'Apple',
          model: macInfo.model,
          hardwareId: macInfo.systemGUID,
          cpuArch: macInfo.arch,
          memoryMb: macInfo.memorySize ~/ (1024 * 1024),
          appVersion: packageInfo.version,
          connectionType: connectionType,
          meshCapable: true, // macOS supports MultipeerConnectivity
        );
      } else if (Platform.isWindows) {
        final windowsInfo = await _deviceInfo.windowsInfo;
        // Build detailed Windows version string
        String osVersion = windowsInfo.productName;
        osVersion += ' (Build ${windowsInfo.buildNumber})';
        if (windowsInfo.displayVersion.isNotEmpty) {
          osVersion += ' ${windowsInfo.displayVersion}';
        }

        _cachedProfile = DeviceProfile(
          platform: 'windows',
          osVersion: osVersion,
          manufacturer: windowsInfo.registeredOwner,
          model: windowsInfo.computerName,
          hardwareId: windowsInfo.deviceId,
          cpuArch: windowsInfo.numberOfCores > 0 ? '${windowsInfo.numberOfCores} cores' : null,
          memoryMb: windowsInfo.systemMemoryInMegabytes,
          appVersion: packageInfo.version,
          connectionType: connectionType,
          meshCapable: true, // Windows can support Bluetooth/WiFi Direct
        );
      } else {
        _cachedProfile = DeviceProfile(
          platform: 'unknown',
          osVersion: 'unknown',
          appVersion: packageInfo.version,
          connectionType: connectionType,
        );
      }

      _logger.i('Device profile collected: $_cachedProfile');
      return _cachedProfile!;
    } catch (e) {
      _logger.e('Error collecting device info: $e');
      final packageInfo = await PackageInfo.fromPlatform();
      return DeviceProfile(
        platform: _getPlatformName(),
        osVersion: 'unknown',
        appVersion: packageInfo.version,
      );
    }
  }

  /// Get the current connection type
  Future<String?> _getConnectionType() async {
    try {
      final connectivity = Connectivity();
      final result = await connectivity.checkConnectivity();

      switch (result) {
        case ConnectivityResult.wifi:
          return 'wifi';
        case ConnectivityResult.mobile:
          return 'cellular';
        case ConnectivityResult.ethernet:
          return 'ethernet';
        case ConnectivityResult.bluetooth:
          return 'bluetooth';
        case ConnectivityResult.vpn:
          return 'vpn';
        case ConnectivityResult.none:
          return 'none';
        default:
          return 'other';
      }
    } catch (e) {
      _logger.w('Could not determine connection type: $e');
      return null;
    }
  }

  /// Get platform name without using Platform (for web compatibility)
  String _getPlatformName() {
    if (kIsWeb) return 'web';
    if (Platform.isAndroid) return 'android';
    if (Platform.isIOS) return 'ios';
    if (Platform.isLinux) return 'linux';
    if (Platform.isMacOS) return 'macos';
    if (Platform.isWindows) return 'windows';
    return 'unknown';
  }

  /// Check if mesh networking is supported on this device
  bool get isMeshCapable => _cachedProfile?.meshCapable ?? false;

  /// Get device name suitable for display
  Future<String> getDeviceName() async {
    final profile = await getDeviceProfile();
    if (profile.model != null) {
      if (profile.manufacturer != null) {
        return '${profile.manufacturer} ${profile.model}';
      }
      return profile.model!;
    }
    return '${profile.platform} device';
  }

  /// Clear cached profile (useful for refresh)
  void clearCache() {
    _cachedProfile = null;
  }
}
