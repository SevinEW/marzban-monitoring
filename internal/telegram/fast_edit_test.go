package telegram

import("testing";"time")

func TestFastEditUsesSharedQueueAndBacksOff(t *testing.T){
 b:=New("TEST_TOKEN",-1);start:=time.Now()
 if err:=b.withGroupEdit(-1,func()error{return nil});err!=nil{t.Fatal(err)}
 if b.groupNext.Before(start.Add(initialEditSpacing))||b.editSpacing!=initialEditSpacing{t.Fatal("edit pacing not applied")}
 b.groupNext=time.Time{}
 start=time.Now()
 _=b.withGroupEdit(-1,func()error{return &APIError{Status:"429 Too Many Requests",RetryAfter:10*time.Second}})
 if b.groupNext.Before(start.Add(11*time.Second))||b.editSpacing<forumWriteSpacing{t.Fatal("flood cooldown or conservative fallback lost")}
 b.groupNext=time.Time{};b.groupBlockedUntil=time.Time{}
 _=b.withGroupEdit(-1,func()error{return nil})
 if b.editSpacing<forumWriteSpacing{t.Fatal("aggressive rate resumed after cooldown")}
}
