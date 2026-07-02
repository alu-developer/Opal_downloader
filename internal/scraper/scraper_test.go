package scraper

import (
	"path/filepath"
	"testing"
)

func TestNormalizePersistentProfileSettings(t *testing.T) {
	tests := []struct {
		name         string
		userDataDir  string
		profileDir   string
		wantUserData string
		wantProfile  string
	}{
		{
			name:         "explicit profile directory wins",
			userDataDir:  "C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data",
			profileDir:   "Profile 2",
			wantUserData: filepath.Clean("C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data"),
			wantProfile:  "Profile 2",
		},
		{
			name:         "infer default profile from full profile path",
			userDataDir:  "C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data/Default",
			wantUserData: filepath.Clean("C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data"),
			wantProfile:  "Default",
		},
		{
			name:         "infer numbered profile from full profile path",
			userDataDir:  "C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data/Profile 1",
			wantUserData: filepath.Clean("C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data"),
			wantProfile:  "Profile 1",
		},
		{
			name:         "leave plain user data dir unchanged",
			userDataDir:  "C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data",
			wantUserData: filepath.Clean("C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data"),
			wantProfile:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUserData, gotProfile := normalizePersistentProfileSettings(tt.userDataDir, tt.profileDir)
			if filepath.Clean(gotUserData) != filepath.Clean(tt.wantUserData) || gotProfile != tt.wantProfile {
				t.Fatalf("normalizePersistentProfileSettings(%q, %q) = (%q, %q), want (%q, %q)", tt.userDataDir, tt.profileDir, gotUserData, gotProfile, tt.wantUserData, tt.wantProfile)
			}
		})
	}
}
