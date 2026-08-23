package profile

import "testing"

func TestApplyLegacyDefaultsDistinguishesMissingFromExplicitFalse(t *testing.T) {
	legacy := Profile{}
	updates := applyLegacyDefaults(&legacy, map[string]interface{}{"nickname": "Luna"})
	if legacy.AvatarType != AvatarDefault || !legacy.ReminderNotificationEnabled || !legacy.ReminderEmailEnabled || len(updates) != 3 {
		t.Fatalf("perfil legado=%#v atualizações=%#v", legacy, updates)
	}

	explicit := Profile{AvatarType: AvatarOrange, ReminderNotificationEnabled: false, ReminderEmailEnabled: false}
	updates = applyLegacyDefaults(&explicit, map[string]interface{}{
		"avatarType":                  AvatarOrange,
		"reminderNotificationEnabled": false,
		"reminderEmailEnabled":        false,
	})
	if explicit.AvatarType != AvatarOrange || explicit.ReminderNotificationEnabled || explicit.ReminderEmailEnabled || len(updates) != 0 {
		t.Fatalf("valores explícitos foram alterados: perfil=%#v atualizações=%#v", explicit, updates)
	}
}
