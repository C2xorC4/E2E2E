import 'package:equatable/equatable.dart';
import 'message.dart';
import 'user.dart';

class Chat extends Equatable {
  final String id;
  final String? name;
  final bool isGroup;
  final DateTime createdAt;
  final List<User> participants;
  final Message? lastMessage;
  final int unreadCount;

  const Chat({
    required this.id,
    this.name,
    required this.isGroup,
    required this.createdAt,
    this.participants = const [],
    this.lastMessage,
    this.unreadCount = 0,
  });

  String get displayName {
    if (name != null && name!.isNotEmpty) return name!;
    if (participants.isNotEmpty) {
      return participants.map((p) => p.username).join(', ');
    }
    return 'Chat';
  }

  factory Chat.fromJson(Map<String, dynamic> json) {
    final chatData = json['chat'] ?? json;
    return Chat(
      id: chatData['id'] as String,
      name: chatData['name'] as String?,
      isGroup: chatData['is_group'] as bool? ?? false,
      createdAt: DateTime.parse(chatData['created_at'] as String),
      participants: (json['participants'] as List<dynamic>?)
              ?.map((p) => User.fromJson(p as Map<String, dynamic>))
              .toList() ??
          [],
      lastMessage: json['last_message'] != null
          ? Message.fromJson(json['last_message'] as Map<String, dynamic>)
          : null,
      unreadCount: json['unread_count'] as int? ?? 0,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'is_group': isGroup,
        'created_at': createdAt.toIso8601String(),
      };

  @override
  List<Object?> get props => [id, name, isGroup];
}
