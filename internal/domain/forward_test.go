package domain

import (
	"errors"
	"testing"
)

func TestForwardSpecSpec(t *testing.T) {
	cases := []struct {
		name string
		spec ForwardSpec
		want string
	}{
		{
			name: "local default bind",
			spec: ForwardSpec{Direction: ForwardLocal, Bind: BindAddr{Port: 5432}, Target: TargetAddr{Host: "db", Port: 5432}},
			want: "127.0.0.1:5432:db:5432",
		},
		{
			name: "local explicit bind",
			spec: ForwardSpec{Direction: ForwardLocal, Bind: BindAddr{Addr: "0.0.0.0", Port: 8080}, Target: TargetAddr{Host: "web", Port: 80}},
			want: "0.0.0.0:8080:web:80",
		},
		{
			name: "remote default bind",
			spec: ForwardSpec{Direction: ForwardRemote, Bind: BindAddr{Port: 9000}, Target: TargetAddr{Host: "localhost", Port: 3000}},
			want: "localhost:9000:localhost:3000",
		},
		{
			name: "dynamic",
			spec: ForwardSpec{Direction: ForwardDynamic, Bind: BindAddr{Port: 1080}},
			want: "127.0.0.1:1080",
		},
		{
			name: "ipv6 target",
			spec: ForwardSpec{Direction: ForwardLocal, Bind: BindAddr{Port: 80}, Target: TargetAddr{Host: "::1", Port: 80}},
			want: "127.0.0.1:80:[::1]:80",
		},
		{
			// Only a remote forward may leave the port to OpenSSH: `-L addr:0:...`
			// is rejected as an invalid specification.
			name: "remote ephemeral port",
			spec: ForwardSpec{Direction: ForwardRemote, Bind: BindAddr{Addr: "localhost", Port: 0}, Target: TargetAddr{Host: "localhost", Port: 3000}},
			want: "localhost:0:localhost:3000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spec.Spec(); got != tc.want {
				t.Errorf("Spec() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestForwardSpecArgs(t *testing.T) {
	spec := ForwardSpec{Direction: ForwardRemote, Bind: BindAddr{Port: 9000}, Target: TargetAddr{Host: "web", Port: 80}}

	args := spec.Args()
	if len(args) != 2 || args[0] != "-R" || args[1] != "localhost:9000:web:80" {
		t.Errorf("Args() = %v, want [-R localhost:9000:web:80]", args)
	}
}

func TestForwardSpecValidate(t *testing.T) {
	valid := []ForwardSpec{
		{Direction: ForwardLocal, Bind: BindAddr{Port: 8080}, Target: TargetAddr{Host: "db", Port: 5432}},
		{Direction: ForwardRemote, Bind: BindAddr{Port: 9000}, Target: TargetAddr{Host: "localhost", Port: 3000}},
		// A remote forward may ask OpenSSH for the port; a local one may not.
		{Direction: ForwardRemote, Bind: BindAddr{Port: 0}, Target: TargetAddr{Host: "localhost", Port: 3000}},
		{Direction: ForwardDynamic, Bind: BindAddr{Port: 1080}},
	}
	for _, spec := range valid {
		if err := spec.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v, want nil", spec.Summary(), err)
		}
	}

	invalid := []ForwardSpec{
		{Direction: ForwardLocal, Bind: BindAddr{Port: 8080}},
		{Direction: ForwardLocal, Bind: BindAddr{Port: 8080}, Target: TargetAddr{Host: "db"}},
		{Direction: ForwardLocal, Bind: BindAddr{Port: 70000}, Target: TargetAddr{Host: "db", Port: 1}},
		// ssh rejects -L with port 0, so the domain rejects it before building the args.
		{Direction: ForwardLocal, Bind: BindAddr{Port: 0}, Target: TargetAddr{Host: "db", Port: 5432}},
		{Direction: ForwardDynamic, Bind: BindAddr{Port: 0}},
		{Direction: ForwardDynamic, Bind: BindAddr{Port: 1080}, Target: TargetAddr{Host: "db", Port: 1}},
		{Direction: ForwardLocal, Bind: BindAddr{Port: 80}, Target: TargetAddr{Host: "two words", Port: 1}},
	}
	for _, spec := range invalid {
		if err := spec.Validate(); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%+v: Validate() = %v, want ErrInvalidArgument", spec, err)
		}
	}
}

func TestForwardSpecID(t *testing.T) {
	spec := ForwardSpec{Direction: ForwardLocal, Bind: BindAddr{Port: 5432}, Target: TargetAddr{Host: "db", Port: 5432}}

	if got, want := spec.ID("gd-vps"), "gd-vps-L5432-5432"; got != want {
		t.Errorf("ID() = %q, want %q", got, want)
	}

	unsafe := spec.ID("host with/slash:and*chars")
	if unsafe == "" {
		t.Fatal("ID() must never be empty")
	}
	for _, r := range unsafe {
		if r == '/' || r == ':' || r == '*' || r == ' ' {
			t.Fatalf("ID() = %q keeps an unsafe character %q", unsafe, r)
		}
	}
}

func TestParseDirection(t *testing.T) {
	cases := map[string]ForwardDirection{
		"L": ForwardLocal, "local": ForwardLocal, "l": ForwardLocal,
		"R": ForwardRemote, "remote": ForwardRemote,
		"D": ForwardDynamic, "dynamic": ForwardDynamic, "socks": ForwardDynamic,
	}
	for raw, want := range cases {
		got, err := ParseDirection(raw)
		if err != nil || got != want {
			t.Errorf("ParseDirection(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}

	if _, err := ParseDirection("x"); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ParseDirection(x) = %v, want ErrInvalidArgument", err)
	}
}

func TestParseForwardSpec(t *testing.T) {
	cases := []struct {
		raw  string
		want ForwardSpec
	}{
		{
			raw:  "L 5432:db.internal:5432",
			want: ForwardSpec{Direction: ForwardLocal, Bind: BindAddr{Addr: "127.0.0.1", Port: 5432}, Target: TargetAddr{Host: "db.internal", Port: 5432}},
		},
		{
			raw:  "pg=L 5432:db.internal:5432",
			want: ForwardSpec{Name: "pg", Direction: ForwardLocal, Bind: BindAddr{Addr: "127.0.0.1", Port: 5432}, Target: TargetAddr{Host: "db.internal", Port: 5432}},
		},
		{
			raw:  "R 9000:localhost:3000",
			want: ForwardSpec{Direction: ForwardRemote, Bind: BindAddr{Addr: "localhost", Port: 9000}, Target: TargetAddr{Host: "localhost", Port: 3000}},
		},
		{
			raw:  "D 1080",
			want: ForwardSpec{Direction: ForwardDynamic, Bind: BindAddr{Addr: "127.0.0.1", Port: 1080}},
		},
		{
			raw:  "local 0.0.0.0:8080:web:80",
			want: ForwardSpec{Direction: ForwardLocal, Bind: BindAddr{Addr: "0.0.0.0", Port: 8080}, Target: TargetAddr{Host: "web", Port: 80}},
		},
		{
			raw:  "L 80:[::1]:8080",
			want: ForwardSpec{Direction: ForwardLocal, Bind: BindAddr{Addr: "127.0.0.1", Port: 80}, Target: TargetAddr{Host: "[::1]", Port: 8080}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := ParseForwardSpec(tc.raw)
			if err != nil {
				t.Fatalf("ParseForwardSpec(%q): %v", tc.raw, err)
			}

			if got.Name != tc.want.Name ||
				got.Direction != tc.want.Direction ||
				got.Bind != tc.want.Bind ||
				got.Target != tc.want.Target {
				t.Errorf("ParseForwardSpec(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseForwardSpecRejects(t *testing.T) {
	cases := []string{
		"",
		"L",
		"L 5432",
		"L 5432:db",
		"X 5432:db:1",
		"L abc:db:1",
		"L 5432:db:notaport",
		"L 70000:db:1",
		"L :db:1",
		"D 1080:extra",
		"L 80:[::1:8080",
	}

	for _, raw := range cases {
		if _, err := ParseForwardSpec(raw); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("ParseForwardSpec(%q) = %v, want ErrInvalidArgument", raw, err)
		}
	}
}

func TestValidateForwardSpecs(t *testing.T) {
	spec := ForwardSpec{Direction: ForwardDynamic, Bind: BindAddr{Port: 1080}}

	if err := ValidateForwardSpecs([]ForwardSpec{spec, spec}); err != nil {
		t.Errorf("ValidateForwardSpecs = %v, want nil", err)
	}

	many := make([]ForwardSpec, MaxForwardSpecs+1)
	for i := range many {
		many[i] = spec
	}
	if err := ValidateForwardSpecs(many); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ValidateForwardSpecs(too many) = %v, want ErrInvalidArgument", err)
	}
}
