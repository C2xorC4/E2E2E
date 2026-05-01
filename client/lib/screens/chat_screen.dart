import 'dart:async';
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import '../models/models.dart';
import '../providers/providers.dart';
import '../services/services.dart';

class ChatScreen extends StatefulWidget {
  const ChatScreen({super.key});

  @override
  State<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends State<ChatScreen> {
  final _messageController = TextEditingController();
  final _scrollController = ScrollController();
  Timer? _typingTimer;

  @override
  void dispose() {
    _messageController.dispose();
    _scrollController.dispose();
    _typingTimer?.cancel();
    super.dispose();
  }

  void _sendMessage() {
    final text = _messageController.text.trim();
    if (text.isEmpty) return;

    context.read<ChatProvider>().sendMessage(text);
    _messageController.clear();

    // Scroll to bottom
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollController.hasClients) {
        _scrollController.animateTo(
          0,
          duration: const Duration(milliseconds: 300),
          curve: Curves.easeOut,
        );
      }
    });
  }

  void _onTyping() {
    final chatProvider = context.read<ChatProvider>();
    chatProvider.sendTyping();

    _typingTimer?.cancel();
    _typingTimer = Timer(const Duration(seconds: 2), () {
      chatProvider.stopTyping();
    });
  }

  void _showChatInfo(BuildContext context, Chat chat) {
    showModalBottomSheet(
      context: context,
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    chat.isGroup ? Icons.group : Icons.person,
                    size: 32,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          chat.displayName,
                          style: Theme.of(context).textTheme.titleLarge,
                        ),
                        Text(
                          chat.isGroup ? 'Group chat' : 'Direct message',
                          style: TextStyle(color: Colors.grey[600]),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
              const Divider(height: 24),
              Text(
                'Participants (${chat.participants.length})',
                style: Theme.of(context).textTheme.titleSmall,
              ),
              const SizedBox(height: 8),
              ...chat.participants.map((user) => ListTile(
                    leading: CircleAvatar(
                      child: Text(user.username[0].toUpperCase()),
                    ),
                    title: Text(user.username),
                    subtitle: Text(user.email),
                    dense: true,
                  )),
              const SizedBox(height: 8),
              Row(
                children: const [
                  Icon(Icons.lock, size: 16, color: Colors.green),
                  SizedBox(width: 8),
                  Text('Messages are end-to-end encrypted'),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  void _handleMenuAction(String action, Chat chat, ChatProvider chatProvider) async {
    switch (action) {
      case 'refresh':
        chatProvider.loadMessages(chat.id, refresh: true);
        break;
      case 'leave':
        _showLeaveConfirmation(context, chat, chatProvider);
        break;
      case 'self_destruct':
        _showSelfDestructConfirmation(context, chat, chatProvider);
        break;
    }
  }

  void _showLeaveConfirmation(BuildContext context, Chat chat, ChatProvider chatProvider) {
    final scaffoldContext = context; // Save outer context
    showDialog(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('Leave Chat'),
        content: const Text(
          'Are you sure you want to leave this chat? You will no longer receive messages from this conversation.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () async {
              Navigator.pop(dialogContext); // Close dialog
              final success = await chatProvider.leaveChat(chat.id);
              if (success && scaffoldContext.mounted) {
                Navigator.pop(scaffoldContext); // Go back to chat list
                ScaffoldMessenger.of(scaffoldContext).showSnackBar(
                  const SnackBar(content: Text('You left the chat')),
                );
              }
            },
            style: TextButton.styleFrom(foregroundColor: Colors.orange),
            child: const Text('Leave'),
          ),
        ],
      ),
    );
  }

  void _showSelfDestructConfirmation(BuildContext context, Chat chat, ChatProvider chatProvider) {
    final scaffoldContext = context; // Save outer context
    showDialog(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('Self-Destruct Chat'),
        content: const Text(
          'This will permanently delete all messages from your device and leave the chat. '
          'Other participants will be notified that you have self-destructed the chat.\n\n'
          'This action cannot be undone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(dialogContext),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () async {
              Navigator.pop(dialogContext); // Close dialog
              final success = await chatProvider.selfDestructChat(chat.id);
              if (success && scaffoldContext.mounted) {
                Navigator.pop(scaffoldContext); // Go back to chat list
                ScaffoldMessenger.of(scaffoldContext).showSnackBar(
                  const SnackBar(content: Text('Chat self-destructed')),
                );
              }
            },
            style: TextButton.styleFrom(foregroundColor: Colors.red),
            child: const Text('Self-Destruct'),
          ),
        ],
      ),
    );
  }

  void _showInviteDialog(BuildContext context, Chat chat, ChatProvider chatProvider) {
    showDialog(
      context: context,
      builder: (context) => _InviteUserDialog(
        chat: chat,
        onInvite: (userId) async {
          final success = await chatProvider.inviteToChat(chat.id, userId);
          if (success && context.mounted) {
            Navigator.pop(context);
            ScaffoldMessenger.of(context).showSnackBar(
              const SnackBar(content: Text('User added to chat')),
            );
          }
        },
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final auth = context.watch<AuthProvider>();
    final chatProvider = context.watch<ChatProvider>();
    final chat = chatProvider.selectedChat;

    if (chat == null) {
      return Scaffold(
        appBar: AppBar(title: const Text('Chat')),
        body: const Center(child: Text('No chat selected')),
      );
    }

    final messages = chatProvider.currentMessages;
    final currentUserId = auth.userId!;

    // Get other participant name for DMs
    String chatName = chat.name ?? '';
    if (chatName.isEmpty) {
      final others = chat.participants.where((p) => p.id != currentUserId);
      chatName = others.map((p) => p.username).join(', ');
    }

    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                if (chat.isGroup)
                  Padding(
                    padding: const EdgeInsets.only(right: 4),
                    child: Icon(
                      Icons.group,
                      size: 16,
                      color: Theme.of(context).colorScheme.primary,
                    ),
                  ),
                Flexible(child: Text(chatName)),
              ],
            ),
            Row(
              children: [
                const Icon(Icons.lock, size: 12, color: Colors.green),
                const SizedBox(width: 4),
                Text(
                  chat.isGroup
                      ? '${chat.participants.length} participants • Encrypted'
                      : 'End-to-end encrypted',
                  style: TextStyle(
                    fontSize: 11,
                    color: Colors.grey[400],
                  ),
                ),
              ],
            ),
          ],
        ),
        actions: [
          if (chat.isGroup)
            IconButton(
              icon: const Icon(Icons.person_add),
              tooltip: 'Add participants',
              onPressed: () => _showInviteDialog(context, chat, chatProvider),
            ),
          IconButton(
            icon: const Icon(Icons.info_outline),
            tooltip: 'Chat info',
            onPressed: () => _showChatInfo(context, chat),
          ),
          PopupMenuButton<String>(
            onSelected: (value) => _handleMenuAction(value, chat, chatProvider),
            itemBuilder: (context) => [
              const PopupMenuItem<String>(
                value: 'refresh',
                child: ListTile(
                  leading: Icon(Icons.refresh),
                  title: Text('Refresh'),
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                ),
              ),
              const PopupMenuDivider(),
              const PopupMenuItem<String>(
                value: 'leave',
                child: ListTile(
                  leading: Icon(Icons.exit_to_app, color: Colors.orange),
                  title: Text('Leave Chat', style: TextStyle(color: Colors.orange)),
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                ),
              ),
              const PopupMenuItem<String>(
                value: 'self_destruct',
                child: ListTile(
                  leading: Icon(Icons.delete_forever, color: Colors.red),
                  title: Text('Self-Destruct', style: TextStyle(color: Colors.red)),
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                ),
              ),
            ],
          ),
        ],
      ),
      body: Column(
        children: [
          // Messages
          Expanded(
            child: chatProvider.isLoading && messages.isEmpty
                ? const Center(child: CircularProgressIndicator())
                : messages.isEmpty
                    ? Center(
                        child: Column(
                          mainAxisAlignment: MainAxisAlignment.center,
                          children: [
                            Icon(
                              Icons.chat_bubble_outline,
                              size: 48,
                              color: Colors.grey[400],
                            ),
                            const SizedBox(height: 16),
                            Text(
                              'No messages yet',
                              style: TextStyle(color: Colors.grey[600]),
                            ),
                            const SizedBox(height: 8),
                            Text(
                              'Send a message to start the conversation',
                              style: TextStyle(
                                color: Colors.grey[500],
                                fontSize: 12,
                              ),
                            ),
                          ],
                        ),
                      )
                    : ListView.builder(
                        controller: _scrollController,
                        reverse: true,
                        padding: const EdgeInsets.all(16),
                        itemCount: messages.length,
                        itemBuilder: (context, index) {
                          final message = messages[index];
                          final isOwn = message.senderId == currentUserId;
                          
                          // Find sender name
                          String senderName = 'Unknown';
                          final sender = chat.participants
                              .where((p) => p.id == message.senderId)
                              .firstOrNull;
                          if (sender != null) {
                            senderName = isOwn ? 'You' : sender.username;
                          }

                          return _MessageBubble(
                            message: message,
                            isOwn: isOwn,
                            senderName: senderName,
                            showSender: chat.isGroup && !isOwn,
                          );
                        },
                      ),
          ),

          // Input
          Container(
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: Theme.of(context).scaffoldBackgroundColor,
              boxShadow: [
                BoxShadow(
                  color: Colors.black.withOpacity(0.1),
                  blurRadius: 4,
                  offset: const Offset(0, -2),
                ),
              ],
            ),
            child: SafeArea(
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _messageController,
                      decoration: InputDecoration(
                        hintText: 'Type a message...',
                        border: OutlineInputBorder(
                          borderRadius: BorderRadius.circular(24),
                          borderSide: BorderSide.none,
                        ),
                        filled: true,
                        fillColor: Colors.grey[200],
                        contentPadding: const EdgeInsets.symmetric(
                          horizontal: 16,
                          vertical: 8,
                        ),
                      ),
                      textInputAction: TextInputAction.send,
                      onSubmitted: (_) => _sendMessage(),
                      onChanged: (_) => _onTyping(),
                      maxLines: null,
                    ),
                  ),
                  const SizedBox(width: 8),
                  CircleAvatar(
                    backgroundColor: Theme.of(context).colorScheme.primary,
                    child: IconButton(
                      icon: const Icon(Icons.send, color: Colors.white),
                      onPressed: _sendMessage,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _MessageBubble extends StatelessWidget {
  final Message message;
  final bool isOwn;
  final String senderName;
  final bool showSender;

  const _MessageBubble({
    required this.message,
    required this.isOwn,
    required this.senderName,
    required this.showSender,
  });

  bool get isSystemMessage =>
      message.contentType.startsWith('system');

  @override
  Widget build(BuildContext context) {
    // System messages are displayed differently
    if (isSystemMessage) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Center(
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
            decoration: BoxDecoration(
              color: Colors.grey[300],
              borderRadius: BorderRadius.circular(16),
            ),
            child: Text(
              message.content,
              style: TextStyle(
                color: Colors.grey[600],
                fontSize: 12,
                fontStyle: FontStyle.italic,
              ),
            ),
          ),
        ),
      );
    }

    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        mainAxisAlignment:
            isOwn ? MainAxisAlignment.end : MainAxisAlignment.start,
        children: [
          Container(
            constraints: BoxConstraints(
              maxWidth: MediaQuery.of(context).size.width * 0.75,
            ),
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
            decoration: BoxDecoration(
              color: isOwn
                  ? Theme.of(context).colorScheme.primary
                  : Colors.grey[300],
              borderRadius: BorderRadius.circular(18).copyWith(
                bottomRight: isOwn ? const Radius.circular(4) : null,
                bottomLeft: !isOwn ? const Radius.circular(4) : null,
              ),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (showSender)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 4),
                    child: Text(
                      senderName,
                      style: TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.bold,
                        color: isOwn ? Colors.white70 : Colors.black54,
                      ),
                    ),
                  ),
                Text(
                  message.content,
                  style: TextStyle(
                    color: isOwn ? Colors.white : Colors.black87,
                  ),
                ),
                const SizedBox(height: 4),
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      _formatTime(message.createdAt),
                      style: TextStyle(
                        fontSize: 10,
                        color: isOwn ? Colors.white60 : Colors.black45,
                      ),
                    ),
                    if (isOwn) ...[
                      const SizedBox(width: 4),
                      Icon(
                        _getStatusIcon(message.status),
                        size: 14,
                        color: Colors.white60,
                      ),
                    ],
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  IconData _getStatusIcon(MessageStatus status) {
    switch (status) {
      case MessageStatus.sending:
        return Icons.access_time;
      case MessageStatus.sent:
        return Icons.check;
      case MessageStatus.delivered:
        return Icons.done_all;
      case MessageStatus.read:
        return Icons.done_all;
      case MessageStatus.failed:
        return Icons.error_outline;
    }
  }

  String _formatTime(DateTime time) {
    return '${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}';
  }
}

class _InviteUserDialog extends StatefulWidget {
  final Chat chat;
  final Future<void> Function(String userId) onInvite;

  const _InviteUserDialog({
    required this.chat,
    required this.onInvite,
  });

  @override
  State<_InviteUserDialog> createState() => _InviteUserDialogState();
}

class _InviteUserDialogState extends State<_InviteUserDialog> {
  final _searchController = TextEditingController();
  List<User> _searchResults = [];
  bool _isSearching = false;
  bool _isInviting = false;

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  Future<void> _search(String query) async {
    if (query.length < 2) {
      setState(() => _searchResults = []);
      return;
    }

    setState(() => _isSearching = true);

    try {
      final api = context.read<ApiService>();
      final results = await api.searchUsers(query);

      // Filter out users already in the chat
      final existingIds = widget.chat.participants.map((p) => p.id).toSet();
      final filtered = results.where((u) => !existingIds.contains(u.id)).toList();

      setState(() {
        _searchResults = filtered;
        _isSearching = false;
      });
    } catch (e) {
      setState(() => _isSearching = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Add Participant'),
      content: SizedBox(
        width: double.maxFinite,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: _searchController,
              decoration: InputDecoration(
                hintText: 'Search users...',
                prefixIcon: const Icon(Icons.search),
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(8),
                ),
              ),
              onChanged: _search,
              autofocus: true,
            ),
            const SizedBox(height: 16),
            if (_isSearching)
              const Center(child: CircularProgressIndicator())
            else if (_searchResults.isEmpty)
              Text(
                _searchController.text.isEmpty
                    ? 'Search for users to add'
                    : 'No users found',
                style: TextStyle(color: Colors.grey[600]),
              )
            else
              ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 200),
                child: ListView.builder(
                  shrinkWrap: true,
                  itemCount: _searchResults.length,
                  itemBuilder: (context, index) {
                    final user = _searchResults[index];
                    return ListTile(
                      leading: CircleAvatar(
                        child: Text(user.username[0].toUpperCase()),
                      ),
                      title: Text(user.username),
                      subtitle: Text(user.email),
                      trailing: _isInviting
                          ? const SizedBox(
                              width: 24,
                              height: 24,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Icon(Icons.add),
                      onTap: _isInviting
                          ? null
                          : () async {
                              setState(() => _isInviting = true);
                              await widget.onInvite(user.id);
                              if (mounted) {
                                setState(() => _isInviting = false);
                              }
                            },
                    );
                  },
                ),
              ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Cancel'),
        ),
      ],
    );
  }
}
