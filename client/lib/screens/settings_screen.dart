import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:provider/provider.dart';

import '../providers/auth_provider.dart';

class SettingsScreen extends StatelessWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final authProvider = context.watch<AuthProvider>();
    final username = authProvider.username ?? 'Unknown';
    final email = authProvider.email ?? '';

    return Scaffold(
      appBar: AppBar(
        title: const Text('Settings'),
      ),
      body: ListView(
        children: [
          // User info section
          ListTile(
            leading: CircleAvatar(
              radius: 28,
              backgroundColor: Colors.blue.shade100,
              child: Text(
                username.isNotEmpty ? username[0].toUpperCase() : '?',
                style: TextStyle(
                  fontSize: 24,
                  color: Colors.blue.shade700,
                  fontWeight: FontWeight.bold,
                ),
              ),
            ),
            title: Text(
              username,
              style: const TextStyle(fontWeight: FontWeight.bold),
            ),
            subtitle: Text(email),
          ),
          const Divider(),

          // Security section
          const Padding(
            padding: EdgeInsets.all(16),
            child: Text(
              'Security',
              style: TextStyle(
                fontWeight: FontWeight.bold,
                color: Colors.grey,
              ),
            ),
          ),
          ListTile(
            leading: const Icon(Icons.fingerprint),
            title: const Text('Key Fingerprint'),
            subtitle: const Text('Verify your identity with contacts'),
            onTap: () => _showKeyFingerprint(context),
          ),
          ListTile(
            leading: const Icon(Icons.devices),
            title: const Text('Active Devices'),
            subtitle: const Text('Current device'),
            onTap: () => _showDevices(context),
          ),
          const Divider(),

          // Privacy section
          const Padding(
            padding: EdgeInsets.all(16),
            child: Text(
              'Privacy',
              style: TextStyle(
                fontWeight: FontWeight.bold,
                color: Colors.grey,
              ),
            ),
          ),
          SwitchListTile(
            secondary: const Icon(Icons.visibility),
            title: const Text('Show Online Status'),
            subtitle: const Text('Let others see when you\'re online'),
            value: true,
            onChanged: (value) {
              // TODO: Implement
            },
          ),
          SwitchListTile(
            secondary: const Icon(Icons.edit),
            title: const Text('Show Typing Indicators'),
            subtitle: const Text('Let others see when you\'re typing'),
            value: true,
            onChanged: (value) {
              // TODO: Implement
            },
          ),
          const Divider(),

          // About section
          const Padding(
            padding: EdgeInsets.all(16),
            child: Text(
              'About',
              style: TextStyle(
                fontWeight: FontWeight.bold,
                color: Colors.grey,
              ),
            ),
          ),
          ListTile(
            leading: const Icon(Icons.info_outline),
            title: const Text('Version'),
            subtitle: const Text('1.0.0'),
          ),
          ListTile(
            leading: const Icon(Icons.lock_outline),
            title: const Text('Encryption'),
            subtitle: const Text('X25519 + XChaCha20-Poly1305'),
          ),
          const Divider(),

          // Logout
          ListTile(
            leading: const Icon(Icons.logout, color: Colors.red),
            title: const Text(
              'Sign Out',
              style: TextStyle(color: Colors.red),
            ),
            onTap: () => _confirmLogout(context),
          ),
        ],
      ),
    );
  }

  void _showKeyFingerprint(BuildContext context) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Your Key Fingerprint'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text(
              'Compare this fingerprint with your contacts to verify your identity:',
              style: TextStyle(fontSize: 14),
            ),
            const SizedBox(height: 16),
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: Colors.grey.shade100,
                borderRadius: BorderRadius.circular(8),
              ),
              child: const SelectableText(
                'Fingerprint not available',
                style: TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 16,
                  fontWeight: FontWeight.bold,
                ),
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }

  void _showDevices(BuildContext context) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Active Devices'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.smartphone),
              title: const Text('Current Device'),
              subtitle: const Text('This device'),
              trailing: Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                decoration: BoxDecoration(
                  color: Colors.green.shade100,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  'Active',
                  style: TextStyle(
                    color: Colors.green.shade700,
                    fontSize: 12,
                  ),
                ),
              ),
            ),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }

  void _confirmLogout(BuildContext context) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Sign Out'),
        content: const Text('Are you sure you want to sign out?'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () {
              context.read<AuthProvider>().logout();
              Navigator.pop(context);
              Navigator.pop(context);
            },
            style: TextButton.styleFrom(foregroundColor: Colors.red),
            child: const Text('Sign Out'),
          ),
        ],
      ),
    );
  }
}
