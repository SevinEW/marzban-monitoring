---
name: humanizer
description: Rewrite AI-sounding text so it reads like the actual writer — plain, specific, varied rhythm, no filler or stock phrases. Works for English and Persian. Use when the user asks to humanize, de-AI, naturalize or make a draft sound like them.
---

# Humanizer

Goal: the same meaning, in the voice of a specific person who knows the subject. Not "trick a detector" — just good, natural writing.

## Process

1. **Find the voice.** If the user gave samples of their own writing, match their sentence length, formality, vocabulary and punctuation habits. Otherwise aim for a clear, direct, slightly informal expert.
2. **Keep every fact, number, name and claim.** Do not add new facts or opinions.
3. **Strip AI tells:**
   - Openers/closers: "In today's fast-paced world", "It's important to note", "In conclusion", "I hope this helps", "Let's dive in".
   - Inflated words: delve, leverage, robust, seamless, crucial, pivotal, tapestry, landscape, realm, elevate, unlock, game-changer, "plays a vital role".
   - Rule-of-three lists everywhere; every paragraph the same length; every sentence starting the same way.
   - "Not only X but also Y", "It's not just X, it's Y" constructions.
   - Excess em-dashes, bolding, emojis and headings in what should be prose.
   - Hedging stacks ("may potentially help to possibly").
4. **Add what humans do:** concrete examples and specifics, varied sentence length (some short), contractions where the register allows, an occasional opinion the writer actually holds, active voice.
5. **Persian:** prefer محاوره‌ای-نوشتاری natural register when the source is informal; avoid literal translations of English idioms and over-use of «همچنین»، «در نهایت»، «لازم به ذکر است»; use correct نیم‌فاصله (می‌شود، ها‌ی جمع).
6. **Read it aloud (mentally).** Anything you would not say to a colleague, rewrite.

## Output

Return only the rewritten text unless the user asks for an explanation. If the text was already natural, say so and make minimal edits.
