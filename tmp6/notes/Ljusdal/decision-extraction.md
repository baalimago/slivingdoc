# Ljusdal KF decision extraction guidance

- KF protocols follow the same shape as neighbouring Hudiksvall: "Kommunfullmäktige
  beslutar" blocks per paragraph (§).
- Keep substantive paragraphs: entlediganden (avsägelser av uppdrag), godkännande av
  årsredovisningar (kommun, bolag, kommunalförbund), ansvarsfrihet (styrelse + alla
  nämnder, listed point by point), antagande av strategier/taxor/planeringsstrategi,
  remissyttranden, medborgarförslag ("anses besvarat" / "bifalles").
- Skip information/notation items even when phrased as "Kommunfullmäktige beslutar":
  "Revisionsberättelsen noteras till protokollet" and "Informationen noteras till
  protokollet" (redovisning av ej färdigbehandlade motioner) — pure notation, no
  substantive outcome. Also skip interpellation paragraphs ("interpellationen får
  framställas" / "anses besvarad") — procedural.
- Voting: omit voting_method unless an explicit omröstning with counts is recorded;
  chair-led propositions without counts → omit.
