part of '../shell.dart';

class _PendingStrip extends StatelessWidget {
  final AppState state;
  const _PendingStrip({required this.state});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(20, 6, 12, 6),
      color: warnColor.withValues(alpha: .12),
      child: Row(
        children: [
          const Icon(Icons.info_outline, size: 16, color: warnColor),
          const SizedBox(width: 10),
          const Expanded(child: Text('Изменения применятся после переподключения', style: TextStyle(fontSize: 13))),
          Btn(label: 'Применить', small: true, onPressed: state.busy ? null : state.reconnect),
        ],
      ),
    );
  }
}

class _Toasts extends StatelessWidget {
  final AppState state;
  const _Toasts({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.end,
      mainAxisSize: MainAxisSize.min,
      children: [
        for (final t in state.toasts)
          Padding(
            key: ValueKey(t.id),
            padding: const EdgeInsets.only(top: 8),
            child: TweenAnimationBuilder<double>(
              tween: Tween(begin: 0, end: 1),
              duration: const Duration(milliseconds: 220),
              builder: (context, v, child) => Opacity(
                opacity: v,
                child: Transform.translate(offset: Offset(0, 8 * (1 - v)), child: child),
              ),
              child: Material(
                color: Colors.transparent,
                child: Container(
                  constraints: const BoxConstraints(maxWidth: 400),
                  padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
                  decoration: BoxDecoration(
                    color: p.surface2,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(
                      color: switch (t.kind) {
                        ToastKind.swap => swapColor.withValues(alpha: .5),
                        ToastKind.err => errColor.withValues(alpha: .5),
                        ToastKind.ok => okColor.withValues(alpha: .45),
                        ToastKind.info => p.border2,
                      },
                    ),
                    boxShadow: [BoxShadow(color: Colors.black.withValues(alpha: .4), blurRadius: 40, offset: const Offset(0, 14), spreadRadius: -12)],
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        switch (t.kind) {
                          ToastKind.swap => Icons.swap_horiz,
                          ToastKind.err => Icons.error_outline,
                          ToastKind.ok => Icons.check_circle_outline,
                          ToastKind.info => Icons.info_outline,
                        },
                        size: 17,
                        color: switch (t.kind) {
                          ToastKind.swap => swapColor,
                          ToastKind.err => errColor,
                          ToastKind.ok => okColor,
                          ToastKind.info => p.muted,
                        },
                      ),
                      const SizedBox(width: 10),
                      Flexible(child: Text(t.message, style: const TextStyle(fontSize: 13))),
                      if (t.action case (final label, final run)) ...[
                        const SizedBox(width: 10),
                        TextButton(
                          onPressed: () {
                            state.dismissToast(t);
                            run();
                          },
                          style: TextButton.styleFrom(
                            foregroundColor: p.accentInk,
                            padding: const EdgeInsets.symmetric(horizontal: 8),
                            minimumSize: const Size(0, 30),
                            tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                          ),
                          // The style on the text: a button's own would drop the theme's font.
                          child: Text(label, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
                        ),
                      ],
                      const SizedBox(width: 6),
                      InkWell(
                        onTap: () => state.dismissToast(t),
                        child: Icon(Icons.close, size: 14, color: p.dim),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
      ],
    );
  }
}
