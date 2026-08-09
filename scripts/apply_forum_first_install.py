from pathlib import Path

main = Path("cmd/marzwatch/main.go")
s = main.read_text()

# Fresh Central installs must configure and validate the live Telegram Forum
# before any config is saved or service is started.
if '"github.com/SevinEW/marzban-monitoring/internal/telegram"' not in s:
    anchor = '\t"github.com/SevinEW/marzban-monitoring/internal/security"\n'
    if anchor not in s:
        raise SystemExit("telegram import anchor not found")
    s = s.replace(
        anchor,
        anchor + '\t"github.com/SevinEW/marzban-monitoring/internal/telegram"\n',
        1,
    )

if 'Telegram Forum Group ID (-100...)' not in s:
    anchor = '''\tadmin, err := strconv.ParseInt(strings.TrimSpace(adminS), 10, 64)\n\tif err != nil {\n\t\tlog.Fatal("Telegram Admin Chat ID dorost nist")\n\t}\n\ttz := ask(r, "Timezone report", "Asia/Tehran")\n'''
    replacement = '''\tadmin, err := strconv.ParseInt(strings.TrimSpace(adminS), 10, 64)\n\tif err != nil {\n\t\tlog.Fatal("Telegram Admin Chat ID dorost nist")\n\t}\n\n\tgroupS := askRequired(r, "Telegram Forum Group ID (-100...)")\n\tgroupID, err := strconv.ParseInt(strings.TrimSpace(groupS), 10, 64)\n\tif err != nil || groupID >= 0 {\n\t\tlog.Fatal("Telegram Forum Group ID dorost nist; bayad ID manfi Supergroup mesle -100... bashad")\n\t}\n\tfmt.Println("🔎 Dar hale check-e Telegram Forum va permission haye bot...")\n\tif err := telegram.New(token, admin).ValidateForum(groupID); err != nil {\n\t\tlog.Fatalf("Telegram Forum validation failed: %v", err)\n\t}\n\tfmt.Println("✅ Telegram Forum validated")\n\n\ttz := ask(r, "Timezone report", "Asia/Tehran")\n'''
    if anchor not in s:
        raise SystemExit("central setup forum prompt anchor not found")
    s = s.replace(anchor, replacement, 1)

if 'TelegramGroupID: groupID,' not in s:
    anchor = '''\t\tTelegramToken: token,\n\t\tAdminChatID:   admin,\n\t\tJoinToken:     join,\n'''
    replacement = '''\t\tTelegramToken:   token,\n\t\tAdminChatID:     admin,\n\t\tTelegramGroupID: groupID,\n\t\tJoinToken:       join,\n'''
    if anchor not in s:
        raise SystemExit("central config forum field anchor not found")
    s = s.replace(anchor, replacement, 1)

main.write_text(s)
