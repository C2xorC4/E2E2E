import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../models/models.dart';
import '../providers/providers.dart';
import '../services/services.dart';

class NewChatScreen extends StatefulWidget {
  const NewChatScreen({super.key});

  @override
  State<NewChatScreen> createState() => _NewChatScreenState();
}

class _NewChatScreenState extends State<NewChatScreen> {
  final _searchController = TextEditingController();
  final _groupNameController = TextEditingController();
  List<User> _searchResults = [];
  List<User> _selectedUsers = [];
  bool _isSearching = false;
  bool _isGroupMode = false;
  String? _error;

  @override
  void dispose() {
    _searchController.dispose();
    _groupNameController.dispose();
    super.dispose();
  }

  Future<void> _search(String query) async {
    if (query.length < 2) {
      setState(() {
        _searchResults = [];
      });
      return;
    }

    setState(() {
      _isSearching = true;
      _error = null;
    });

    try {
      final api = context.read<ApiService>();
      final results = await api.searchUsers(query);
      
      // Filter out current user
      final auth = context.read<AuthProvider>();
      final filtered = results.where((u) => u.id != auth.userId).toList();

      setState(() {
        _searchResults = filtered;
        _isSearching = false;
      });
    } catch (e) {
      setState(() {
        _error = e.toString();
        _isSearching = false;
      });
    }
  }

  void _toggleUserSelection(User user) {
    setState(() {
      if (_selectedUsers.any((u) => u.id == user.id)) {
        _selectedUsers.removeWhere((u) => u.id == user.id);
      } else {
        _selectedUsers.add(user);
      }
      // Auto-switch to group mode when multiple users selected
      _isGroupMode = _selectedUsers.length > 1;
    });
  }

  Future<void> _startDirectChat(User user) async {
    final chatProvider = context.read<ChatProvider>();

    // Create or find existing chat
    final chat = await chatProvider.createChat([user.id]);

    if (chat != null && mounted) {
      await chatProvider.selectChat(chat);
      if (mounted) {
        Navigator.pushReplacementNamed(context, '/chat');
      }
    }
  }

  Future<void> _createGroupChat() async {
    if (_selectedUsers.isEmpty) {
      setState(() => _error = 'Select at least one user');
      return;
    }

    final groupName = _groupNameController.text.trim();
    if (groupName.isEmpty) {
      setState(() => _error = 'Enter a group name');
      return;
    }

    final chatProvider = context.read<ChatProvider>();
    final chat = await chatProvider.createGroupChat(
      groupName,
      _selectedUsers.map((u) => u.id).toList(),
    );

    if (chat != null && mounted) {
      await chatProvider.selectChat(chat);
      if (mounted) {
        Navigator.pushReplacementNamed(context, '/chat');
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(_isGroupMode ? 'New Group Chat' : 'New Chat'),
        actions: [
          // Toggle group mode
          IconButton(
            icon: Icon(_isGroupMode ? Icons.person : Icons.group_add),
            tooltip: _isGroupMode ? 'Direct Message' : 'Create Group',
            onPressed: () {
              setState(() {
                _isGroupMode = !_isGroupMode;
                if (!_isGroupMode) {
                  _selectedUsers.clear();
                }
              });
            },
          ),
        ],
      ),
      body: Column(
        children: [
          // Selected users (for group mode)
          if (_isGroupMode && _selectedUsers.isNotEmpty) ...[
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              color: Theme.of(context).colorScheme.surfaceContainerHighest,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Selected (${_selectedUsers.length})',
                    style: TextStyle(
                      fontSize: 12,
                      color: Colors.grey[600],
                    ),
                  ),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 4,
                    children: _selectedUsers.map((user) {
                      return Chip(
                        label: Text(user.username),
                        deleteIcon: const Icon(Icons.close, size: 16),
                        onDeleted: () => _toggleUserSelection(user),
                      );
                    }).toList(),
                  ),
                ],
              ),
            ),
            // Group name input
            Padding(
              padding: const EdgeInsets.all(16),
              child: TextField(
                controller: _groupNameController,
                decoration: InputDecoration(
                  hintText: 'Group name',
                  prefixIcon: const Icon(Icons.group),
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(12),
                  ),
                ),
              ),
            ),
          ],

          // Search bar
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
            child: TextField(
              controller: _searchController,
              decoration: InputDecoration(
                hintText: 'Search users...',
                prefixIcon: const Icon(Icons.search),
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(12),
                ),
                suffixIcon: _searchController.text.isNotEmpty
                    ? IconButton(
                        icon: const Icon(Icons.clear),
                        onPressed: () {
                          _searchController.clear();
                          setState(() {
                            _searchResults = [];
                          });
                        },
                      )
                    : null,
              ),
              onChanged: _search,
              autofocus: true,
            ),
          ),

          // Error message
          if (_error != null)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Text(
                _error!,
                style: const TextStyle(color: Colors.red),
              ),
            ),

          // Results
          Expanded(
            child: _isSearching
                ? const Center(child: CircularProgressIndicator())
                : _searchResults.isEmpty
                    ? Center(
                        child: Column(
                          mainAxisAlignment: MainAxisAlignment.center,
                          children: [
                            Icon(
                              _isGroupMode ? Icons.group_add : Icons.person_search,
                              size: 64,
                              color: Colors.grey[400],
                            ),
                            const SizedBox(height: 16),
                            Text(
                              _searchController.text.isEmpty
                                  ? _isGroupMode
                                      ? 'Search for users to add to the group'
                                      : 'Search for users to start a conversation'
                                  : 'No users found',
                              style: TextStyle(color: Colors.grey[600]),
                            ),
                          ],
                        ),
                      )
                    : ListView.builder(
                        itemCount: _searchResults.length,
                        itemBuilder: (context, index) {
                          final user = _searchResults[index];
                          final isSelected = _selectedUsers.any((u) => u.id == user.id);

                          return ListTile(
                            leading: CircleAvatar(
                              backgroundColor: isSelected
                                  ? Theme.of(context).colorScheme.primary
                                  : Theme.of(context).colorScheme.primaryContainer,
                              child: isSelected
                                  ? const Icon(Icons.check, color: Colors.white)
                                  : Text(
                                      user.username[0].toUpperCase(),
                                      style: TextStyle(
                                        color: Theme.of(context)
                                            .colorScheme
                                            .onPrimaryContainer,
                                      ),
                                    ),
                            ),
                            title: Text(user.username),
                            subtitle: Text(user.email),
                            trailing: _isGroupMode
                                ? Checkbox(
                                    value: isSelected,
                                    onChanged: (_) => _toggleUserSelection(user),
                                  )
                                : const Icon(Icons.chat_bubble_outline),
                            onTap: () {
                              if (_isGroupMode) {
                                _toggleUserSelection(user);
                              } else {
                                _startDirectChat(user);
                              }
                            },
                          );
                        },
                      ),
          ),
        ],
      ),
      // Create group button
      floatingActionButton: _isGroupMode && _selectedUsers.isNotEmpty
          ? FloatingActionButton.extended(
              onPressed: _createGroupChat,
              icon: const Icon(Icons.check),
              label: const Text('Create Group'),
            )
          : null,
    );
  }
}
