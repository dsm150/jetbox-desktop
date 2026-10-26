//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// Кнопка строки меню нашего NSStatusItem: Wails её не отдаёт, а элемент в процессе один.
// Кнопка лежит в окне NSStatusBarWindow, глубже contentView — ищем по дереву.
static NSStatusBarButton *jbFindButton(NSView *v) {
	if ([v isKindOfClass:[NSStatusBarButton class]]) return (NSStatusBarButton *)v;
	for (NSView *sub in v.subviews) {
		NSStatusBarButton *b = jbFindButton(sub);
		if (b != nil) return b;
	}
	return nil;
}

static NSStatusBarButton *jbStatusButton(void) {
	for (NSWindow *w in [NSApp windows]) {
		if (![NSStringFromClass([w class]) isEqualToString:@"NSStatusBarWindow"]) continue;
		NSStatusBarButton *b = jbFindButton(w.contentView.superview ?: w.contentView);
		if (b != nil) return b;
	}
	return nil;
}

// Цветной значок, который сам выбирает вариант под вид строки меню: обработчик рисования
// вызывается с внешним видом кнопки (светлая или тёмная строка — в том числе от обоев),
// поэтому не нужен шаблон (он перекрасил бы синий след в серый).
static bool jbSetStatusImage(const void *light, int lightLen, const void *dark, int darkLen) {
	// До [NSApp run] главная очередь не обслуживается — dispatch_sync повис бы.
	if (NSApp == nil || ![NSApp isRunning]) return false;
	NSData *ld = [NSData dataWithBytes:light length:lightLen];
	NSData *dd = [NSData dataWithBytes:dark length:darkLen];
	__block bool ok = false;
	void (^apply)(void) = ^{
		NSStatusBarButton *button = jbStatusButton();
		if (button == nil) return;
		NSImage *li = [[NSImage alloc] initWithData:ld];
		NSImage *di = [[NSImage alloc] initWithData:dd];
		CGFloat side = [[NSStatusBar systemStatusBar] thickness];
		NSImage *img = [NSImage imageWithSize:NSMakeSize(side, side) flipped:NO drawingHandler:^BOOL(NSRect rect) {
			NSAppearanceName name = [[NSAppearance currentDrawingAppearance]
				bestMatchFromAppearancesWithNames:@[NSAppearanceNameAqua, NSAppearanceNameDarkAqua]];
			NSImage *src = [name isEqualToString:NSAppearanceNameDarkAqua] ? di : li;
			[src drawInRect:rect];
			return YES;
		}];
		img.template = NO;
		button.image = img;
		ok = true;
	};
	if ([NSThread isMainThread]) apply();
	else dispatch_sync(dispatch_get_main_queue(), apply);
	return ok;
}
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// setTrayImage — значок строки меню под её вид (светлая/тёмная). До запуска приложения
// кнопки ещё нет — тогда значок уходит в Wails (светлый, без шаблона) и возвращается
// false: трей повторит, когда кнопка появится. Шаблонный значок Wails (SetTemplateIcon)
// не используется: флаг шаблона у форка залипает и красит синий след в серый.
func setTrayImage(st *application.SystemTray, light, dark []byte) bool {
	if C.jbSetStatusImage(unsafe.Pointer(&light[0]), C.int(len(light)), unsafe.Pointer(&dark[0]), C.int(len(dark))) {
		return true
	}
	st.SetIcon(light)
	return false
}
