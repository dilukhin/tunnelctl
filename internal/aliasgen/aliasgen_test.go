package aliasgen

import "testing"

func TestGenerateYandexAliasFromKey(t *testing.T) {
	used := map[string]bool{}
	got := Generate(Input{
		User: "dilukhin",
		Host: "158.160.79.19",
		Key:  "~/.ssh/id_yandex_vm",
	}, used)

	if got.Alias != "yandex" {
		t.Fatalf("ожидался алиас yandex, получено %q", got.Alias)
	}
	if got.Name != "yandex-158-160-79-19" {
		t.Fatalf("ожидалось имя yandex-158-160-79-19, получено %q", got.Name)
	}
}

func TestGenerateTunnelAliasFromGenericKeyAndUser(t *testing.T) {
	used := map[string]bool{}
	got := Generate(Input{
		User: "tunneluser",
		Host: "45.10.164.145",
		Key:  "~/.ssh/id_ed25519",
	}, used)

	if got.Alias != "ip-45-10" {
		t.Fatalf("ожидался алиас ip-45-10 из IP, получено %q", got.Alias)
	}
	if got.Name != "45-10-164-145" {
		t.Fatalf("ожидалось имя 45-10-164-145, получено %q", got.Name)
	}
}

func TestGenerateUniqueAlias(t *testing.T) {
	used := map[string]bool{"yandex": true, "yandex-158": true}
	got := Generate(Input{
		User: "dilukhin",
		Host: "158.160.79.19",
		Key:  "~/.ssh/id_yandex_vm",
	}, used)

	if got.Alias != "yandex-2" {
		t.Fatalf("ожидался алиас yandex-2 при конфликте, получено %q", got.Alias)
	}
}
