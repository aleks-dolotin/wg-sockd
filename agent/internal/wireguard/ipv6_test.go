package wireguard

import "testing"

func TestDeriveIPv6(t *testing.T) {
	tests := []struct {
		name          string
		clientAddress string
		prefix        string
		want          string
		wantErr       bool
	}{
		{
			name:          "typical /24 peer",
			clientAddress: "10.0.3.2/24",
			prefix:        "fd00:ab01::",
			want:          "fd00:ab01::2/128",
		},
		{
			name:          "high octet /24",
			clientAddress: "10.0.3.254/24",
			prefix:        "fd00:ab01::",
			want:          "fd00:ab01::254/128",
		},
		{
			name:          "host address /32",
			clientAddress: "10.0.3.2/32",
			prefix:        "fd00:ab01::",
			want:          "fd00:ab01::2/128",
		},
		{
			name:          "/16 subnet",
			clientAddress: "10.0.3.2/16",
			prefix:        "fd00:ab01::",
			want:          "fd00:ab01::770/128", // 3*256 + 2 = 770
		},
		{
			name:          "prefix empty — disabled",
			clientAddress: "10.0.3.2/24",
			prefix:        "",
			want:          "",
		},
		{
			name:          "client address empty",
			clientAddress: "",
			prefix:        "fd00:ab01::",
			want:          "",
		},
		{
			name:          "IPv6 input — skip",
			clientAddress: "fe80::1/64",
			prefix:        "fd00:ab01::",
			want:          "",
		},
		{
			name:          "invalid CIDR",
			clientAddress: "not-a-cidr",
			prefix:        "fd00:ab01::",
			wantErr:       true,
		},
		{
			name:          "first host in /24",
			clientAddress: "10.0.3.1/24",
			prefix:        "fd00:ab01::",
			want:          "fd00:ab01::1/128",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DeriveIPv6(tt.clientAddress, tt.prefix)
			if tt.wantErr {
				if err == nil {
					t.Errorf("DeriveIPv6(%q, %q) expected error, got nil", tt.clientAddress, tt.prefix)
				}
				return
			}
			if err != nil {
				t.Errorf("DeriveIPv6(%q, %q) unexpected error: %v", tt.clientAddress, tt.prefix, err)
				return
			}
			if got != tt.want {
				t.Errorf("DeriveIPv6(%q, %q) = %q, want %q", tt.clientAddress, tt.prefix, got, tt.want)
			}
		})
	}
}
