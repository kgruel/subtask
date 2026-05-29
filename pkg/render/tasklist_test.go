package render

import (
	"strings"
	"testing"
)

// TestColorStatusByEnum pins that list status color is selected by the
// UserStatus enum value, not by re-parsing the display text, and that the
// display text is passed through verbatim (text and color are decoupled).
func TestColorStatusByEnum(t *testing.T) {
	cases := []struct {
		name string
		enum string
		text string
		want string
	}{
		{"working", "working", "working (3m)", styleStatusWorking.Render("working (3m)")},
		{"replied", "replied", "replied (2m)", styleStatusReplied.Render("replied (2m)")},
		{"error", "error", "error (2m)", styleStatusError.Render("error (2m)")},
		// "interrupted" text rides the error enum and must color identically.
		{"interrupted-as-error", "error", "interrupted (2m)", styleStatusError.Render("interrupted (2m)")},
		{"merged", "merged", "✓ merged", styleStatusMerged.Render("✓ merged")},
		{"closed", "closed", "closed", styleStatusClosed.Render("closed")},
		{"draft", "draft", "draft", styleStatusDraft.Render("draft")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := colorStatusByEnum(tc.enum, tc.text)
			if got != tc.want {
				t.Errorf("colorStatusByEnum(%q, %q) = %q, want %q", tc.enum, tc.text, got, tc.want)
			}
			// Display text survives intact regardless of the color wrapper.
			if !strings.Contains(got, tc.text) {
				t.Errorf("colorStatusByEnum(%q, %q) = %q, expected to contain %q", tc.enum, tc.text, got, tc.text)
			}
		})
	}
}

// TestColorStatusByEnum_Fallback pins the default branch: an empty/unknown enum
// with empty or dash text renders the dim em-dash, matching the prior behavior
// for tasks with no status text.
func TestColorStatusByEnum_Fallback(t *testing.T) {
	for _, text := range []string{"", "-", "—"} {
		got := colorStatusByEnum("", text)
		want := styleDim.Render("—")
		if got != want {
			t.Errorf("colorStatusByEnum(\"\", %q) = %q, want %q", text, got, want)
		}
	}
}
