package masking

import "testing"

func TestOrderedFieldPolicy(t *testing.T) {
	rules := []Rule{{" EMAIL, phone.* ", Transform{FixedChar, "base"}}, {"email", Transform{HideMiddle, "short"}}}
	p, err := Compile(rules)
	if err != nil {
		t.Fatal(err)
	}
	rules[1].Transform.Pattern = "changed"
	for _, tc := range []struct {
		names       []string
		value, want string
		hit         bool
	}{
		{[]string{"EMAIL", "alias"}, "管🙂员", "管*员", true},
		{[]string{"alias", "Email"}, "李", "short", true},
		{[]string{"Phone.home"}, "1234", "base", true},
		{[]string{"xemail", "phoneXhome"}, "", "", false},
	} {
		m, ok := p.Match(tc.names...)
		if ok != tc.hit || ok && m.Text(tc.value) != tc.want {
			t.Errorf("%v: got %v %v", tc.names, m, ok)
		}
	}
	var empty Policy
	if empty.Enabled() {
		t.Fatal("zero policy enabled")
	}
	for _, fields := range []string{"  ", " , , ", string([]byte{0xff})} {
		if _, err := Compile([]Rule{{Fields: fields}}); err == nil {
			t.Fatalf("accepted invalid field pattern %q", fields)
		}
	}
}
