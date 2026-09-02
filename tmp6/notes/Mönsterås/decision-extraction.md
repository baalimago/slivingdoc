# Mönsterås KF decision extraction guidance

- KF protocols ("Sammanträdesprotokoll för Kommunfullmäktige", Hotell Munken) record a
  "Beslut" block per §; keep each § with a Beslut block as one decision, paragraph_number
  "§N" matching the innehållsförteckning.
- Include procedural opening items (§ Val av justerare, § Fastställande av dagordning)
  and elections (§ Valärenden) — explicit decision outcomes.
- Include interpellation items: both filings ("lägger interpellation och svar till
  handlingarna") and receipts ("tar emot interpellationen").
- Include remittering items: motioner/medborgarförslag lämnas till kommunstyrelsen
  (för beredning / för handläggning).
- Skip pure information items even with a Beslut block: "intygar att de tagit del av
  informationen" (e.g. genomlysning status report) and "Information från revisorerna"
  (also when it utgår). Skip § Nästa möte (announcement of next date only).
- Chair-led propositions without a counted votering: omit voting_method. Reservations
  (t.ex. SD reserverar sig) are not part of the decision text; skip.
- Politicians: "Beslutande" two-column roster; persons marked "tjänstgörande ersättare"
  get role Tjänstgörande ersättare + tag. Ordförande/vice ordförande appear in the
  justering/signature block (Jens Robertsson ordförande, 1:e/2:e vice ordförande).
  Övriga närvarande (officials, revisorer) are skipped.
