# Mariestad KF decision extraction guidance

- Standard KF protocol layout: each paragraph has "Kommunfullmäktiges beslut"
  section followed by Bakgrund, Kommunstyrelsens förslag, Behandling på
  sammanträdet, Underlag, "Beslutet ska skickas till".
- Decisions are passed by chair-led proposition ("finner att kommunfullmäktige
  beslutar i enlighet med förslaget") without counted votes -> omit voting_method.
- Skip § "Anmälningsärenden" whose outcome is only "Besluten anmäls i
  kommunfullmäktige och läggs till handlingarna" (announcements, not decisions).
- Skip "Inga frågor/interpellationer har inkommit" and "Inga nya motioner
  anmäls" statements.
- Medborgarförslag paragraph "överlämna ... till nämnd för beredning och beslut"
  IS a decision (extract).
- Motionssvar "avslå motionen" are decisions; reservations are recorded but do
  not change the outcome.
- Fyllnadsval/entledigande paragraphs: keep all numbered beslut points in full_text.
