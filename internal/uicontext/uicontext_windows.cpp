// UI Automation capture of the text around the caret in the focused control,
// the Windows counterpart of the Linux AT-SPI path (atspi_linux.go). We read a
// short window of text on each side of the selection/caret via
// IUIAutomationTextPattern and hand the boundary characters to the shared
// transform (transform.go) which decides capitalisation / spacing / tail
// punctuation. Controls without a TextPattern (terminals, custom-drawn fields)
// report hasContext=0 and the dictation is pasted unchanged.

#include "uicontext_windows.h"

#include <windows.h>
#include <uiautomation.h>
#include <cwctype>
#include <string>

// GUIDs defined locally so the build doesn't depend on MinGW shipping the
// UIAutomation import library (libuuid coverage varies).
static const GUID kCLSID_CUIAutomation =
    {0xff48dba4, 0x60ef, 0x4201, {0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}};
static const GUID kIID_IUIAutomation =
    {0x30cbe57d, 0xd9d0, 0x452a, {0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}};
static const GUID kIID_IUIAutomationTextPattern =
    {0x32eba289, 0x3583, 0x42c9, {0x9c, 0x59, 0x3b, 0x6d, 0x9a, 0x1e, 0x9b, 0x6a}};
static const GUID kIID_IUIAutomationValuePattern =
    {0xa94cd8b1, 0x0844, 0x4cd6, {0x9d, 0x2d, 0x64, 0x05, 0x37, 0xab, 0x39, 0xe9}};

// How many characters to read on each side of the caret. We only need the
// boundary, so a small window keeps GetText cheap even in huge documents.
static const int kWindow = 16;

// trimWide returns s with leading/trailing whitespace removed.
static std::wstring trimWide(const wchar_t* s, UINT len) {
	UINT a = 0, b = len;
	while (a < b && iswspace(s[a])) a++;
	while (b > a && iswspace(s[b - 1])) b--;
	return std::wstring(s + a, s + b);
}

// looksEmptyOrPlaceholder reports whether the focused editable element is
// actually empty even though its TextPattern may report placeholder text.
// Chromium/Electron rich editors (e.g. VS Code / Electron chat inputs) hand
// the placeholder label back as document text with the caret pinned after it,
// which otherwise reads as a mid-sentence insert. Signals, strongest first:
//   - ValuePattern value is empty (the real value, placeholder excluded);
//   - the document text is blank or a single U+FFFC embed placeholder;
//   - the document text equals the element's Name/HelpText (placeholder shown
//     as the accessible label of an empty field).
static bool looksEmptyOrPlaceholder(IUIAutomationElement* el, IUIAutomationTextPattern* tp) {
	// ValuePattern: most reliable. Inputs/textareas expose the true value.
	IUnknown* vunk = NULL;
	if (SUCCEEDED(el->GetCurrentPattern(UIA_ValuePatternId, &vunk)) && vunk) {
		IUIAutomationValuePattern* vp = NULL;
		if (SUCCEEDED(vunk->QueryInterface(kIID_IUIAutomationValuePattern, (void**)&vp)) && vp) {
			BSTR val = NULL;
			bool empty = false;
			if (SUCCEEDED(vp->get_CurrentValue(&val))) {
				empty = (val == NULL) || trimWide(val, SysStringLen(val)).empty();
			}
			if (val) SysFreeString(val);
			vp->Release();
			if (empty) {
				vunk->Release();
				return true; // empty value is a strong "empty" signal
			}
			// A non-empty value is NOT a reliable "has content" signal for
			// contenteditables (they can report placeholder text), so fall
			// through to the document / name checks below.
		}
		vunk->Release();
	}

	// No ValuePattern (e.g. a contenteditable): inspect the document text.
	IUIAutomationTextRange* doc = NULL;
	if (FAILED(tp->get_DocumentRange(&doc)) || doc == NULL) {
		return false;
	}
	BSTR docText = NULL;
	doc->GetText(512, &docText);

	// Placeholder text (the empty-field hint in web/Electron editors) is
	// read-only; real typed content is not. If the whole document range reads
	// as read-only, the field is effectively empty and just showing its
	// placeholder — treat it as a fresh start. UIA_IsReadOnlyAttributeId =
	// 40015; the value is VT_BOOL (VARIANT_TRUE when read-only), or the
	// reserved "mixed" object when the range isn't uniform (left as false).
	bool readOnly = false;
	VARIANT v;
	VariantInit(&v);
	if (SUCCEEDED(doc->GetAttributeValue(40015, &v))) {
		if (v.vt == VT_BOOL && v.boolVal != VARIANT_FALSE) {
			readOnly = true;
		}
	}
	VariantClear(&v);
	doc->Release();

	if (docText == NULL) {
		return readOnly;
	}
	UINT len = SysStringLen(docText);
	std::wstring trimmed = trimWide(docText, len);
	bool hasFFFC = false;
	for (UINT i = 0; i < len; i++) {
		if (docText[i] == 0xFFFC) hasFFFC = true;
	}
	bool result = false;
	if (trimmed.empty() || hasFFFC || readOnly) {
		result = true;
	} else {
		BSTR name = NULL, help = NULL;
		el->get_CurrentName(&name);
		el->get_CurrentHelpText(&help);
		if (name && trimWide(name, SysStringLen(name)) == trimmed) result = true;
		if (help && trimWide(help, SysStringLen(help)) == trimmed) result = true;
		if (name) SysFreeString(name);
		if (help) SysFreeString(help);
	}
	SysFreeString(docText);
	return result;
}

extern "C" int mur_uictx_capture(MurUICtx* out) {
	ZeroMemory(out, sizeof(*out));

	HRESULT hrInit = CoInitializeEx(NULL, COINIT_APARTMENTTHREADED);
	// S_OK/S_FALSE: we initialised COM and must balance it. RPC_E_CHANGED_MODE:
	// the thread already has COM up in another mode — usable, but not ours.
	bool didInit = (hrInit == S_OK || hrInit == S_FALSE);

	IUIAutomation* automation = NULL;
	IUIAutomationElement* el = NULL;
	IUnknown* unk = NULL;
	IUIAutomationTextPattern* tp = NULL;
	IUIAutomationTextRangeArray* sel = NULL;
	IUIAutomationTextRange* range = NULL;
	int result = 0;

	// stage records how far we got, so a "no text focus" in the log says
	// exactly why: 1=COM, 2=no focused element, 3=control has no TextPattern,
	// 4=QI failed, 5=GetSelection failed, 6=empty selection, 0=ok.
	out->stage = 1;
	if (FAILED(CoCreateInstance(kCLSID_CUIAutomation, NULL, CLSCTX_INPROC_SERVER,
	                            kIID_IUIAutomation, (void**)&automation)) ||
	    automation == NULL) {
		goto cleanup;
	}
	out->stage = 2;
	if (FAILED(automation->GetFocusedElement(&el)) || el == NULL) {
		goto cleanup;
	}
	out->stage = 3;
	if (FAILED(el->GetCurrentPattern(UIA_TextPatternId, &unk)) || unk == NULL) {
		goto cleanup; // not a text control — pass the dictation through unchanged
	}
	out->stage = 4;
	if (FAILED(unk->QueryInterface(kIID_IUIAutomationTextPattern, (void**)&tp)) || tp == NULL) {
		goto cleanup;
	}
	out->stage = 5;
	if (FAILED(tp->GetSelection(&sel)) || sel == NULL) {
		goto cleanup;
	}
	out->stage = 6;
	{
		int n = 0;
		sel->get_Length(&n);
		if (n < 1 || FAILED(sel->GetElement(0, &range)) || range == NULL) {
			goto cleanup;
		}
	}
	out->stage = 0;

	out->hasContext = 1;
	out->rightKnown = 1;

	// An empty field that only shows placeholder text (VS Code and similar
	// Electron chat inputs) must read as a fresh
	// start, not a mid-sentence insert — otherwise the placeholder's last
	// character becomes a bogus "preceding" and the dictation gets a leading
	// space / lower-cased first letter.
	if (looksEmptyOrPlaceholder(el, tp)) {
		out->atStart = 1;
		out->atEnd = 1;
		result = 1;
		goto cleanup;
	}

	// --- left side: text from up to kWindow chars back, ending at sel start ---
	{
		IUIAutomationTextRange* before = NULL;
		if (SUCCEEDED(range->Clone(&before)) && before != NULL) {
			int moved = 0;
			before->MoveEndpointByRange(TextPatternRangeEndpoint_End, range,
			                            TextPatternRangeEndpoint_Start);
			before->MoveEndpointByUnit(TextPatternRangeEndpoint_Start,
			                           TextUnit_Character, -kWindow, &moved);
			BSTR text = NULL;
			before->GetText(-1, &text);
			UINT len = text ? SysStringLen(text) : 0;
			// Strip trailing spaces/tabs (not newlines — a newline is a line
			// boundary the transform treats specially).
			UINT i = len;
			while (i > 0 && (text[i - 1] == L' ' || text[i - 1] == L'\t')) {
				i--;
				out->spaceBefore = 1;
			}
			if (i == 0) {
				out->atStart = 1;
			} else {
				wchar_t c = text[i - 1];
				out->preceding = (c == L'\n' || c == L'\r') ? '\n' : (int)c;
			}
			if (text) SysFreeString(text);
			before->Release();
		}
	}

	// --- right side: text from sel end forward up to kWindow chars ---
	{
		IUIAutomationTextRange* after = NULL;
		if (SUCCEEDED(range->Clone(&after)) && after != NULL) {
			int moved = 0;
			after->MoveEndpointByRange(TextPatternRangeEndpoint_Start, range,
			                           TextPatternRangeEndpoint_End);
			after->MoveEndpointByUnit(TextPatternRangeEndpoint_End,
			                          TextUnit_Character, kWindow, &moved);
			BSTR text = NULL;
			after->GetText(-1, &text);
			UINT len = text ? SysStringLen(text) : 0;
			if (len == 0) {
				out->atEnd = 1;
			} else {
				out->following = (int)text[0]; // deliberately not blank-skipped
			}
			if (text) SysFreeString(text);
			after->Release();
		}
	}

	result = 1;

cleanup:
	if (range) range->Release();
	if (sel) sel->Release();
	if (tp) tp->Release();
	if (unk) unk->Release();
	if (el) el->Release();
	if (automation) automation->Release();
	if (didInit) CoUninitialize();
	return result;
}
