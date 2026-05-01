import 'package:equatable/equatable.dart';

class User extends Equatable {
  final String id;
  final String username;
  final String email;
  final String? publicKey;
  final DateTime createdAt;
  final DateTime? lastSeen;

  const User({
    required this.id,
    required this.username,
    required this.email,
    this.publicKey,
    required this.createdAt,
    this.lastSeen,
  });

  factory User.fromJson(Map<String, dynamic> json) {
    return User(
      id: json['id'] as String,
      username: json['username'] as String,
      email: json['email'] as String? ?? '',
      publicKey: json['public_key'] as String?,
      createdAt: DateTime.parse(json['created_at'] as String),
      lastSeen: json['last_seen'] != null
          ? DateTime.parse(json['last_seen'] as String)
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'username': username,
        'email': email,
        'public_key': publicKey,
        'created_at': createdAt.toIso8601String(),
        'last_seen': lastSeen?.toIso8601String(),
      };

  @override
  List<Object?> get props => [id, username, email];
}
