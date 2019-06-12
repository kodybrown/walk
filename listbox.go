// Copyright 2012 The Walk Authors. All rights reserved.
// Use of lb source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build windows

package walk

import (
	"fmt"
	"math/big"
	"reflect"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

type ListBox struct {
	WidgetBase
	model                           ListModel
	providedModel                   interface{}
	styler                          ListItemStyler
	style                           ListItemStyle
	dataMember                      string
	format                          string
	precision                       int
	prevCurIndex                    int
	itemsResetHandlerHandle         int
	itemChangedHandlerHandle        int
	itemsInsertedHandlerHandle      int
	itemsRemovedHandlerHandle       int
	maxItemTextWidth                int
	lastWidth                       int
	lastWidthsMeasuredFor           []int
	currentIndexChangedPublisher    EventPublisher
	selectedIndexesChangedPublisher EventPublisher
	itemActivatedPublisher          EventPublisher
	themeNormalBGColor              Color
	themeNormalTextColor            Color
	themeSelectedBGColor            Color
	themeSelectedTextColor          Color
	themeSelectedNotFocusedBGColor  Color
}

func NewListBox(parent Container) (*ListBox, error) {
	return NewListBoxWithStyle(parent, 0)
}

func NewListBoxWithStyle(parent Container, style uint32) (*ListBox, error) {
	lb := new(ListBox)

	err := InitWidget(
		lb,
		parent,
		"LISTBOX",
		win.WS_BORDER|win.WS_TABSTOP|win.WS_VISIBLE|win.WS_VSCROLL|win.WS_HSCROLL|win.LBS_NOINTEGRALHEIGHT|win.LBS_NOTIFY|style,
		0)
	if err != nil {
		return nil, err
	}

	succeeded := false
	defer func() {
		if !succeeded {
			lb.Dispose()
		}
	}()

	lb.style.dpi = lb.DPI()

	lb.ApplySysColors()

	lb.GraphicsEffects().Add(InteractionEffect)
	lb.GraphicsEffects().Add(FocusEffect)

	lb.MustRegisterProperty("CurrentIndex", NewProperty(
		func() interface{} {
			return lb.CurrentIndex()
		},
		func(v interface{}) error {
			return lb.SetCurrentIndex(assertIntOr(v, -1))
		},
		lb.CurrentIndexChanged()))

	lb.MustRegisterProperty("CurrentItem", NewReadOnlyProperty(
		func() interface{} {
			if i := lb.CurrentIndex(); i > -1 {
				if rm, ok := lb.providedModel.(reflectModel); ok {
					return reflect.ValueOf(rm.Items()).Index(i).Interface()
				}
			}

			return nil
		},
		lb.CurrentIndexChanged()))

	lb.MustRegisterProperty("HasCurrentItem", NewReadOnlyBoolProperty(
		func() bool {
			return lb.CurrentIndex() != -1
		},
		lb.CurrentIndexChanged()))

	succeeded = true

	return lb, nil
}

func (*ListBox) LayoutFlags() LayoutFlags {
	return ShrinkableHorz | ShrinkableVert | GrowableHorz | GrowableVert | GreedyHorz | GreedyVert
}

func (lb *ListBox) ItemStyler() ListItemStyler {
	return lb.styler
}

func (lb *ListBox) SetItemStyler(styler ListItemStyler) {
	lb.styler = styler
}

func (lb *ListBox) ApplySysColors() {
	lb.WidgetBase.ApplySysColors()

	lb.themeNormalBGColor = Color(win.GetSysColor(win.COLOR_WINDOW))
	lb.themeNormalTextColor = Color(win.GetSysColor(win.COLOR_WINDOWTEXT))
	lb.themeSelectedBGColor = Color(win.GetSysColor(win.COLOR_HIGHLIGHT))
	lb.themeSelectedTextColor = Color(win.GetSysColor(win.COLOR_HIGHLIGHTTEXT))
	lb.themeSelectedNotFocusedBGColor = Color(win.GetSysColor(win.COLOR_BTNFACE))
}

func (lb *ListBox) ApplyDPI(dpi int) {
	lb.style.dpi = dpi

	lb.WidgetBase.ApplyDPI(dpi)
}

func (lb *ListBox) applyFont(font *Font) {
	lb.WidgetBase.applyFont(font)

	for i := range lb.lastWidthsMeasuredFor {
		lb.lastWidthsMeasuredFor[i] = 0
	}
}

func (lb *ListBox) itemString(index int) string {
	switch val := lb.model.Value(index).(type) {
	case string:
		return val

	case time.Time:
		return val.Format(lb.format)

	case *big.Rat:
		return val.FloatString(lb.precision)

	default:
		return fmt.Sprintf(lb.format, val)
	}

	panic("unreachable")
}

//insert one item from list model
func (lb *ListBox) insertItemAt(index int) error {
	str := lb.itemString(index)
	lp := uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(str)))
	ret := int(lb.SendMessage(win.LB_INSERTSTRING, uintptr(index), lp))
	if ret == win.LB_ERRSPACE || ret == win.LB_ERR {
		return newError("SendMessage(LB_INSERTSTRING)")
	}
	return nil
}

func (lb *ListBox) removeItem(index int) error {
	if win.LB_ERR == int(lb.SendMessage(win.LB_DELETESTRING, uintptr(index), 0)) {
		return newError("SendMessage(LB_DELETESTRING)")
	}

	return nil
}

// reread all the items from list model
func (lb *ListBox) resetItems() error {
	lb.SetSuspended(true)
	defer lb.SetSuspended(false)

	lb.SendMessage(win.LB_RESETCONTENT, 0, 0)

	lb.maxItemTextWidth = 0

	lb.SetCurrentIndex(-1)

	if lb.model == nil {
		return nil
	}

	count := lb.model.ItemCount()

	lb.lastWidthsMeasuredFor = make([]int, count)

	for i := 0; i < count; i++ {
		if err := lb.insertItemAt(i); err != nil {
			return err
		}
	}

	if lb.styler == nil {
		// Update the listbox width (this sets the correct horizontal scrollbar).
		sh := lb.SizeHint()
		lb.SendMessage(win.LB_SETHORIZONTALEXTENT, uintptr(sh.Width), 0)
	}

	return nil
}

func (lb *ListBox) ensureVisibleItemsHeightUpToDate() error {
	if lb.styler == nil {
		return nil
	}

	lb.SetSuspended(true)
	defer lb.SetSuspended(false)

	offset := int(lb.SendMessage(win.LB_GETTOPINDEX, 0, 0))
	count := lb.model.ItemCount()
	var rc win.RECT
	lb.SendMessage(win.LB_GETITEMRECT, uintptr(offset), uintptr(unsafe.Pointer(&rc)))
	width := lb.IntTo96DPI(int(rc.Right - rc.Left))
	offsetTop := int(rc.Top)
	lbHeight := lb.HeightPixels()

	for i := offset; i >= 0 && i < count; i++ {
		if lb.lastWidthsMeasuredFor[i] == lb.lastWidth {
			continue
		}

		lb.SendMessage(win.LB_GETITEMRECT, uintptr(i), uintptr(unsafe.Pointer(&rc)))

		if int(rc.Top)-offsetTop > lbHeight {
			break
		}

		height := lb.IntFrom96DPI(lb.styler.ItemHeight(i, width))

		lb.SendMessage(win.LB_SETITEMHEIGHT, uintptr(i), uintptr(height))

		lb.lastWidthsMeasuredFor[i] = lb.lastWidth
	}

	lb.SendMessage(win.LB_SETTOPINDEX, uintptr(offset), 0)

	return nil
}

func (lb *ListBox) attachModel() {
	itemsResetHandler := func() {
		lb.resetItems()
	}
	lb.itemsResetHandlerHandle = lb.model.ItemsReset().Attach(itemsResetHandler)

	itemChangedHandler := func(index int) {
		if win.CB_ERR == lb.SendMessage(win.LB_DELETESTRING, uintptr(index), 0) {
			newError("SendMessage(CB_DELETESTRING)")
		}

		lb.insertItemAt(index)

		lb.SetCurrentIndex(lb.prevCurIndex)
	}
	lb.itemChangedHandlerHandle = lb.model.ItemChanged().Attach(itemChangedHandler)

	lb.itemsInsertedHandlerHandle = lb.model.ItemsInserted().Attach(func(from, to int) {
		for i := from; i <= to; i++ {
			lb.insertItemAt(i)
		}
	})

	lb.itemsRemovedHandlerHandle = lb.model.ItemsRemoved().Attach(func(from, to int) {
		for i := to; i >= from; i-- {
			lb.removeItem(i)
		}
	})
}

func (lb *ListBox) detachModel() {
	lb.model.ItemsReset().Detach(lb.itemsResetHandlerHandle)
	lb.model.ItemChanged().Detach(lb.itemChangedHandlerHandle)
	lb.model.ItemsInserted().Detach(lb.itemsInsertedHandlerHandle)
	lb.model.ItemsRemoved().Detach(lb.itemsRemovedHandlerHandle)
}

// Model returns the model of the ListBox.
func (lb *ListBox) Model() interface{} {
	return lb.providedModel
}

// SetModel sets the model of the ListBox.
//
// It is required that mdl either implements walk.ListModel or
// walk.ReflectListModel or be a slice of pointers to struct or a []string.
func (lb *ListBox) SetModel(mdl interface{}) error {
	model, ok := mdl.(ListModel)
	if !ok && mdl != nil {
		var err error
		if model, err = newReflectListModel(mdl); err != nil {
			return err
		}

		if _, ok := mdl.([]string); !ok {
			if badms, ok := model.(bindingAndDisplayMemberSetter); ok {
				badms.setDisplayMember(lb.dataMember)
			}
		}
	}
	lb.providedModel = mdl

	if lb.model != nil {
		lb.detachModel()
	}

	lb.model = model

	if model != nil {
		lb.attachModel()
	}

	if err := lb.resetItems(); err != nil {
		return err
	}

	return lb.ensureVisibleItemsHeightUpToDate()
}

// DataMember returns the member from the model of the ListBox that is displayed
// in the ListBox.
//
// This is only applicable to walk.ReflectListModel models and simple slices of
// pointers to struct.
func (lb *ListBox) DataMember() string {
	return lb.dataMember
}

// SetDataMember sets the member from the model of the ListBox that is displayed
// in the ListBox.
//
// This is only applicable to walk.ReflectListModel models and simple slices of
// pointers to struct.
//
// For a model consisting of items of type S, the type of the specified member T
// and dataMember "Foo", this can be one of the following:
//
//	A field		Foo T
//	A method	func (s S) Foo() T
//	A method	func (s S) Foo() (T, error)
//
// If dataMember is not a simple member name like "Foo", but a path to a
// member like "A.B.Foo", members "A" and "B" both must be one of the options
// mentioned above, but with T having type pointer to struct.
func (lb *ListBox) SetDataMember(dataMember string) error {
	if dataMember != "" {
		if _, ok := lb.providedModel.([]string); ok {
			return newError("invalid for []string model")
		}
	}

	lb.dataMember = dataMember

	if badms, ok := lb.model.(bindingAndDisplayMemberSetter); ok {
		badms.setDisplayMember(dataMember)
	}

	return nil
}

func (lb *ListBox) Format() string {
	return lb.format
}

func (lb *ListBox) SetFormat(value string) {
	lb.format = value
}

func (lb *ListBox) Precision() int {
	return lb.precision
}

func (lb *ListBox) SetPrecision(value int) {
	lb.precision = value
}

func (lb *ListBox) calculateMaxItemTextWidth() int {
	hdc := win.GetDC(lb.hWnd)
	if hdc == 0 {
		newError("GetDC failed")
		return -1
	}
	defer win.ReleaseDC(lb.hWnd, hdc)

	hFontOld := win.SelectObject(hdc, win.HGDIOBJ(lb.Font().handleForDPI(0)))
	defer win.SelectObject(hdc, hFontOld)

	var maxWidth int

	if lb.model == nil {
		return -1
	}
	count := lb.model.ItemCount()
	for i := 0; i < count; i++ {
		item := lb.itemString(i)
		var s win.SIZE
		str := syscall.StringToUTF16(item)

		if !win.GetTextExtentPoint32(hdc, &str[0], int32(len(str)-1), &s) {
			newError("GetTextExtentPoint32 failed")
			return -1
		}

		maxWidth = maxi(maxWidth, int(s.CX))
	}

	return maxWidth
}

func (lb *ListBox) SizeHint() Size {
	defaultSize := lb.dialogBaseUnitsToPixels(Size{50, 12})

	if lb.maxItemTextWidth <= 0 {
		lb.maxItemTextWidth = lb.calculateMaxItemTextWidth()
	}

	// FIXME: Use GetThemePartSize instead of guessing
	w := maxi(defaultSize.Width, lb.maxItemTextWidth+24)
	h := defaultSize.Height + 1

	return Size{w, h}
}

func (lb *ListBox) CurrentIndex() int {
	return int(int32(lb.SendMessage(win.LB_GETCURSEL, 0, 0)))
}

func (lb *ListBox) SetCurrentIndex(value int) error {
	if value > -1 && win.LB_ERR == int(int32(lb.SendMessage(win.LB_SETCURSEL, uintptr(value), 0))) {
		return newError("Invalid index or ensure lb is single-selection listbox")
	}

	if value != lb.prevCurIndex {
		lb.prevCurIndex = value
		lb.currentIndexChangedPublisher.Publish()
	}

	return nil
}

func (lb *ListBox) SelectedIndexes() []int {
	count := int(int32(lb.SendMessage(win.LB_GETCOUNT, 0, 0)))
	if count < 1 {
		return nil
	}
	index32 := make([]int32, count)
	if n := int(int32(lb.SendMessage(win.LB_GETSELITEMS, uintptr(count), uintptr(unsafe.Pointer(&index32[0]))))); n == win.LB_ERR {
		return nil
	} else {
		indexes := make([]int, n)
		for i := 0; i < n; i++ {
			indexes[i] = int(index32[i])
		}
		return indexes
	}
}

func (lb *ListBox) SetSelectedIndexes(indexes []int) {
	var m int32 = -1
	lb.SendMessage(win.LB_SETSEL, win.FALSE, uintptr(m))
	for _, v := range indexes {
		lb.SendMessage(win.LB_SETSEL, win.TRUE, uintptr(uint32(v)))
	}
	lb.selectedIndexesChangedPublisher.Publish()
}

func (lb *ListBox) CurrentIndexChanged() *Event {
	return lb.currentIndexChangedPublisher.Event()
}

func (lb *ListBox) SelectedIndexesChanged() *Event {
	return lb.selectedIndexesChangedPublisher.Event()
}

func (lb *ListBox) ItemActivated() *Event {
	return lb.itemActivatedPublisher.Event()
}

func (lb *ListBox) WndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_MEASUREITEM:
		if lb.styler == nil {
			break
		}

		mis := (*win.MEASUREITEMSTRUCT)(unsafe.Pointer(lParam))

		mis.ItemHeight = uint32(lb.IntFrom96DPI(lb.styler.DefaultItemHeight()))

		return win.TRUE

	case win.WM_DRAWITEM:
		dis := (*win.DRAWITEMSTRUCT)(unsafe.Pointer(lParam))

		if lb.styler == nil || dis.ItemID < 0 {
			break
		}

		lb.style.index = int(dis.ItemID)
		lb.style.rc = dis.RcItem
		lb.style.bounds = lb.RectangleTo96DPI(rectangleFromRECT(dis.RcItem))
		lb.style.state = dis.ItemState
		lb.style.hdc = dis.HDC

		if dis.ItemAction == win.ODA_FOCUS {
			return win.TRUE
		}

		if dis.ItemState&win.ODS_CHECKED != 0 {
			if lb.Focused() {
				lb.style.BackgroundColor = lb.themeSelectedBGColor
				lb.style.TextColor = lb.themeSelectedTextColor
			} else {
				lb.style.BackgroundColor = lb.themeSelectedNotFocusedBGColor
				lb.style.TextColor = lb.themeNormalTextColor
			}
		} else {
			lb.style.BackgroundColor = lb.themeNormalBGColor
			lb.style.TextColor = lb.themeNormalTextColor
		}

		lb.style.DrawBackground()
		if lb.style.Focused() {
			lb.style.DrawFocusRectangle()
		}

		lb.styler.StyleItem(&lb.style)

		defer func() {
			lb.style.bounds = Rectangle{}
			if lb.style.canvas != nil {
				lb.style.canvas.Dispose()
				lb.style.canvas = nil
			}
			lb.style.hdc = 0
		}()

		return win.TRUE

	case win.WM_SIZE:
		if lb.styler != nil && lb.styler.ItemHeightDependsOnWidth() {
			width := lb.WidthPixels()
			if width != lb.lastWidth {
				lb.lastWidth = width
				lb.lastWidthsMeasuredFor = make([]int, lb.model.ItemCount())
			}
		}

		lb.ensureVisibleItemsHeightUpToDate()

	case win.WM_VSCROLL:
		lb.ensureVisibleItemsHeightUpToDate()

	case win.WM_MOUSEWHEEL:
		lb.ensureVisibleItemsHeightUpToDate()

	case win.WM_COMMAND:
		switch win.HIWORD(uint32(wParam)) {
		case win.LBN_SELCHANGE:
			lb.ensureVisibleItemsHeightUpToDate()
			lb.prevCurIndex = lb.CurrentIndex()
			lb.currentIndexChangedPublisher.Publish()
			lb.selectedIndexesChangedPublisher.Publish()

		case win.LBN_DBLCLK:
			lb.itemActivatedPublisher.Publish()
		}

	case win.WM_GETDLGCODE:
		if form := ancestor(lb); form != nil {
			if dlg, ok := form.(dialogish); ok {
				if dlg.DefaultButton() != nil {
					// If the ListBox lives in a Dialog that has a DefaultButton,
					// we won't swallow the return key.
					break
				}
			}
		}

		if wParam == win.VK_RETURN {
			return win.DLGC_WANTALLKEYS
		}

	case win.WM_KEYDOWN:
		if uint32(lParam)>>30 == 0 && Key(wParam) == KeyReturn && lb.CurrentIndex() > -1 {
			lb.itemActivatedPublisher.Publish()
		}
	}

	return lb.WidgetBase.WndProc(hwnd, msg, wParam, lParam)
}
