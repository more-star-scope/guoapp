import 'package:flutter/material.dart';

import 'playback_preferences.dart';

Future<int?> showPlaybackSeekDialog(BuildContext context, int seconds) =>
    showDialog<int>(
      context: context,
      builder: (_) => _PlaybackSeekDialog(seconds: seconds),
    );

class _PlaybackSeekDialog extends StatefulWidget {
  const _PlaybackSeekDialog({required this.seconds});
  final int seconds;

  @override
  State<_PlaybackSeekDialog> createState() => _PlaybackSeekDialogState();
}

class _PlaybackSeekDialogState extends State<_PlaybackSeekDialog> {
  final _form = GlobalKey<FormState>();
  late final TextEditingController _seconds;

  @override
  void initState() {
    super.initState();
    final text = '${widget.seconds}';
    _seconds = TextEditingController(text: text)
      ..selection = TextSelection(baseOffset: 0, extentOffset: text.length);
  }

  @override
  void dispose() {
    _seconds.dispose();
    super.dispose();
  }

  void _save() {
    if (_form.currentState?.validate() != true) return;
    Navigator.pop(context, int.parse(_seconds.text, radix: 10));
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('自定义快进 / 快退'),
    content: Form(
      key: _form,
      child: TextFormField(
        key: const ValueKey('custom-seek-seconds'),
        controller: _seconds,
        autofocus: true,
        keyboardType: TextInputType.number,
        textInputAction: TextInputAction.done,
        decoration: const InputDecoration(
          labelText: '每次跳转秒数',
          suffixText: '秒',
          helperText: '$minPlaybackSeekSeconds–$maxPlaybackSeekSeconds 秒（整数）',
        ),
        autovalidateMode: AutovalidateMode.onUserInteraction,
        validator: (text) {
          final value = int.tryParse(text ?? '', radix: 10);
          if (value == null ||
              value < minPlaybackSeekSeconds ||
              value > maxPlaybackSeekSeconds) {
            return '请输入 $minPlaybackSeekSeconds–$maxPlaybackSeekSeconds 的整数';
          }
          return null;
        },
        onFieldSubmitted: (_) => _save(),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(
        key: const ValueKey('custom-seek-save'),
        onPressed: _save,
        child: const Text('保存'),
      ),
    ],
  );
}
