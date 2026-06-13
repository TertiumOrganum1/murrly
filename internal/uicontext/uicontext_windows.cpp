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
static const GUID kIID_IUIAutomationLegacyIAccessiblePattern =
    {0x828055ad, 0x355b, 0x4435, {0x86, 0xd5, 0x3b, 0x51, 0xc1, 0x4a, 0x9b, 0x1b}};

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
static std::wstring bstrToW(BSTR b) {
	return b ? std::wstring(b, SysStringLen(b)) : std::wstring();
}

// getStrProp reads a string (BSTR) UIA property off an element, or "".
static std::wstring getStrProp(IUIAutomationElement* el, PROPERTYID id) {
	VARIANT v;
	VariantInit(&v);
	std::wstring out;
	if (SUCCEEDED(el->GetCurrentPropertyValue(id, &v)) && v.vt == VT_BSTR && v.bstrVal) {
		out = std::wstring(v.bstrVal, SysStringLen(v.bstrVal));
	}
	VariantClear(&v);
	return out;
}

static std::wstring trimW(const std::wstring& s) {
	return trimWide(s.c_str(), (UINT)s.size());
}

// isKnownPlaceholder matches placeholder hints from chat editors that expose
// the placeholder AS the field value, with no structural flag (read-only,
// aria-placeholder, empty value/legacy/MSAA) to tell it apart from real
// content. Matching them lets such an empty field read as a fresh start rather
// than a mid-sentence insert. Matched by case-insensitive prefix so we don't
// store vendor names verbatim and minor placeholder wording still matches; if
// a new placeholder appears, the log (out->dbg) shows it and it's one line to
// add here.
static bool isKnownPlaceholder(const std::wstring& s) {
	if (s.empty()) return false;
	std::wstring low;
	low.reserve(s.size());
	for (wchar_t c : s) low += (wchar_t)towlower(c);
	static const wchar_t* prefixes[] = {
		L"ctrl esc to focus or unfocus",
		L"queue another message",
	};
	for (const wchar_t* p : prefixes) {
		std::wstring pp(p);
		if (low.size() >= pp.size() && low.compare(0, pp.size(), pp) == 0) {
			return true;
		}
	}
	return false;
}

// setDbg copies a UTF-16 diagnostic summary into out->dbg (truncated).
static void setDbg(MurUICtx* out, const std::wstring& s) {
	size_t n = s.size();
	if (n > 255) n = 255;
	for (size_t i = 0; i < n; i++) out->dbg[i] = (unsigned short)s[i];
	out->dbg[n] = 0;
}

// looksEmptyOrPlaceholder gathers every "is this field actually empty" signal
// and records a diagnostic summary in out->dbg for the log. An empty field
// that shows placeholder text (chat inputs etc.) must read as a fresh start,
// not a mid-sentence insert.
static bool looksEmptyOrPlaceholder(IUIAutomationElement* el, IUIAutomationTextPattern* tp, MurUICtx* out) {
	bool hasVP = false, valEmpty = false;
	std::wstring valStr;
	IUnknown* vunk = NULL;
	if (SUCCEEDED(el->GetCurrentPattern(UIA_ValuePatternId, &vunk)) && vunk) {
		IUIAutomationValuePattern* vp = NULL;
		if (SUCCEEDED(vunk->QueryInterface(kIID_IUIAutomationValuePattern, (void**)&vp)) && vp) {
			hasVP = true;
			BSTR val = NULL;
			if (SUCCEEDED(vp->get_CurrentValue(&val))) {
				valStr = bstrToW(val);
				valEmpty = trimW(valStr).empty();
			}
			if (val) SysFreeString(val);
			vp->Release();
		}
		vunk->Release();
	}

	// LegacyIAccessible value = the MSAA value, which usually reflects the
	// real (typed) content rather than the placeholder TextPattern reports.
	bool hasLegacy = false, legacyEmpty = false;
	std::wstring legacyVal;
	IUnknown* lunk = NULL;
	if (SUCCEEDED(el->GetCurrentPattern(UIA_LegacyIAccessiblePatternId, &lunk)) && lunk) {
		IUIAutomationLegacyIAccessiblePattern* lp = NULL;
		if (SUCCEEDED(lunk->QueryInterface(kIID_IUIAutomationLegacyIAccessiblePattern, (void**)&lp)) && lp) {
			hasLegacy = true;
			BSTR lv = NULL;
			if (SUCCEEDED(lp->get_CurrentValue(&lv))) {
				legacyVal = bstrToW(lv);
				legacyEmpty = trimW(legacyVal).empty();
			}
			if (lv) SysFreeString(lv);
			lp->Release();
		}
		lunk->Release();
	}

	std::wstring docStr;
	bool readOnly = false, hasFFFC = false;
	IUIAutomationTextRange* doc = NULL;
	if (SUCCEEDED(tp->get_DocumentRange(&doc)) && doc) {
		BSTR docText = NULL;
		doc->GetText(512, &docText);
		docStr = bstrToW(docText);
		if (docText) SysFreeString(docText);
		for (size_t i = 0; i < docStr.size(); i++) {
			if (docStr[i] == 0xFFFC) hasFFFC = true;
		}
		// UIA_IsReadOnlyAttributeId = 40015. Placeholder text is read-only,
		// real typed content is not.
		VARIANT v;
		VariantInit(&v);
		if (SUCCEEDED(doc->GetAttributeValue(40015, &v))) {
			if (v.vt == VT_BOOL && v.boolVal != VARIANT_FALSE) readOnly = true;
		}
		VariantClear(&v);
		doc->Release();
	}

	BSTR name = NULL, help = NULL;
	el->get_CurrentName(&name);
	el->get_CurrentHelpText(&help);
	std::wstring nameStr = bstrToW(name), helpStr = bstrToW(help);
	if (name) SysFreeString(name);
	if (help) SysFreeString(help);

	// AriaProperties (UIA_AriaPropertiesPropertyId = 30102) is a "k=v;k=v"
	// string; Chromium puts the field's placeholder there. If the document /
	// value text equals that placeholder, the field is empty and just showing
	// the hint — the reliable, non-hardcoded signal we were missing.
	std::wstring ariaStr = getStrProp(el, 30102);
	std::wstring fullStr = getStrProp(el, 40034); // UIA_FullDescriptionPropertyId
	std::wstring ph;
	{
		size_t p = ariaStr.find(L"placeholder=");
		if (p != std::wstring::npos) {
			size_t s = p + 12; // strlen("placeholder=")
			size_t e = ariaStr.find(L';', s);
			ph = trimW(ariaStr.substr(s, e == std::wstring::npos ? std::wstring::npos : e - s));
		}
	}

	std::wstring docTrim = trimW(docStr);
	bool nameMatches = !docTrim.empty() && (trimW(nameStr) == docTrim || trimW(helpStr) == docTrim);
	bool placeholderShown = !ph.empty() && (docTrim == ph || trimW(valStr) == ph);
	bool knownPh = isKnownPlaceholder(docTrim) || isKnownPlaceholder(trimW(valStr));
	// Decide "empty" only from signals that don't contradict a non-empty
	// document. ValuePattern/MSAA-empty are deliberately NOT used: Qt apps
	// (Telegram) report an empty value/legacy even when the field HAS text,
	// which would wrongly treat a real mid-sentence insert as a fresh start.
	// If the TextPattern document has real text, the field is not empty.
	bool empty = docTrim.empty() || hasFFFC || readOnly || nameMatches || placeholderShown || knownPh;

	std::wstring dbg = L"vp=";
	dbg += hasVP ? (valEmpty ? L"<empty>" : (L"'" + valStr + L"'")) : L"none";
	dbg += L" legacy=";
	dbg += hasLegacy ? (legacyEmpty ? L"<empty>" : (L"'" + legacyVal + L"'")) : L"none";
	dbg += L" ro=" + std::wstring(readOnly ? L"1" : L"0");
	dbg += L" fffc=" + std::wstring(hasFFFC ? L"1" : L"0");
	dbg += L" doc='" + docStr + L"'";
	dbg += L" name='" + nameStr + L"' help='" + helpStr + L"'";
	dbg += L" aria='" + ariaStr + L"' ph='" + ph + L"' full='" + fullStr + L"'";
	dbg += L" => empty=" + std::wstring(empty ? L"1" : L"0");
	setDbg(out, dbg);

	return empty;
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
	if (looksEmptyOrPlaceholder(el, tp, out)) {
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
