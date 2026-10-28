import 'video_enhancement_preferences.dart';

const playbackSpeeds = [.5, .75, 1.0, 1.25, 1.5, 2.0, 3.0];
const playbackSeekSteps = [5, 10, 15, 30, 60];
const minPlaybackSeekSeconds = 1;
const maxPlaybackSeekSeconds = 600;

class PlaybackPreferences {
  const PlaybackPreferences({
    this.speed = 1,
    this.seekSeconds = 5,
    this.quality = 0,
    this.autoAdvance = true,
    this.danmaku = true,
    this.preload = true,
    this.enhancement = const VideoEnhancementPreferences(),
  });

  final double speed;
  final int seekSeconds;
  final int quality;
  final bool autoAdvance;
  final bool danmaku;
  final bool preload;
  final VideoEnhancementPreferences enhancement;

  PlaybackPreferences copyWith({
    double? speed,
    int? seekSeconds,
    int? quality,
    bool? autoAdvance,
    bool? danmaku,
    bool? preload,
    VideoEnhancementPreferences? enhancement,
  }) => PlaybackPreferences(
    speed: speed ?? this.speed,
    seekSeconds: seekSeconds ?? this.seekSeconds,
    quality: quality ?? this.quality,
    autoAdvance: autoAdvance ?? this.autoAdvance,
    danmaku: danmaku ?? this.danmaku,
    preload: preload ?? this.preload,
    enhancement: enhancement ?? this.enhancement,
  );

  Map<String, dynamic> toJson() => {
    'speed': speed,
    'seekSeconds': seekSeconds,
    'quality': quality,
    'autoAdvance': autoAdvance,
    'danmaku': danmaku,
    'preload': preload,
    'enhancement': enhancement.toJson(),
  };

  factory PlaybackPreferences.fromJson(Map<String, dynamic> value) {
    final speed = (value['speed'] as num? ?? 1).toDouble();
    final seekSeconds = value['seekSeconds'] as int? ?? 5;
    final quality = value['quality'] as int? ?? 0;
    final autoAdvance = value['autoAdvance'] as bool? ?? true;
    final danmaku = value['danmaku'] as bool? ?? true;
    final preload = value['preload'] as bool? ?? true;
    if (!playbackSpeeds.contains(speed) ||
        seekSeconds < minPlaybackSeekSeconds ||
        seekSeconds > maxPlaybackSeekSeconds ||
        quality < 0 ||
        quality > 4320) {
      throw const FormatException('播放偏好无效');
    }
    return PlaybackPreferences(
      speed: speed,
      seekSeconds: seekSeconds,
      quality: quality,
      autoAdvance: autoAdvance,
      danmaku: danmaku,
      preload: preload,
      enhancement: VideoEnhancementPreferences.fromJson(value['enhancement']),
    );
  }
}
