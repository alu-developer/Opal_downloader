package scraper

import "testing"

func TestStrictCourseFilterMatches(t *testing.T) {
	tests := []struct {
		name   string
		course string
		filter []string
		want   bool
	}{
		{name: "exact match", course: "Programmierung 1", filter: []string{"Programmierung 1"}, want: true},
		{name: "case-insensitive exact", course: "Programmierung 1", filter: []string{"programmierung 1"}, want: true},
		{name: "partial should not match", course: "Programmierung 1", filter: []string{"Programmierung"}, want: false},
		{name: "wildcard all still allowed", course: "Programmierung 1", filter: []string{"*"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strictCourseFilterMatches(tt.course, tt.filter); got != tt.want {
				t.Fatalf("strictCourseFilterMatches(%q, %#v) = %v, want %v", tt.course, tt.filter, got, tt.want)
			}
		})
	}
}
