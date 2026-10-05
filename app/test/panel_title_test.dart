import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/ui/theme.dart';
import 'package:coreshift/ui/widgets.dart';

void main() {
  // A panel of a two-column page at 1280 px: the title and its subtitle
  // shared the width, and both were cut, «Сайты и адреса без …».
  testWidgets('a panel title keeps its words; the subtitle gives way', (tester) async {
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    const title = 'Сайты и адреса без VPN', sub = 'сайт и все его поддомены, и ещё немного слов';
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(Brightness.dark),
        home: Scaffold(
          body: Center(
            child: SizedBox(width: 420, child: PanelTitle(title, sub: sub)),
          ),
        ),
      ),
    );
    RenderParagraph para(String text) => tester.renderObject<RenderParagraph>(find.text(text));
    expect(para(title).didExceedMaxLines, isFalse, reason: 'the title was cut');
    expect(para(sub).didExceedMaxLines, isTrue, reason: 'the subtitle should give way');
    expect(tester.takeException(), isNull);

    // Too narrow even for the title: it is cut, nothing overflows.
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(Brightness.dark),
        home: Scaffold(
          body: Center(
            child: SizedBox(width: 120, child: PanelTitle(title, sub: sub)),
          ),
        ),
      ),
    );
    expect(para(title).didExceedMaxLines, isTrue);
    expect(tester.takeException(), isNull);
  });
}
