# Köping KF decision extraction guidance

- Köping KF protocols use standard layout: each § has a "Beslut" block;
  extract those blocks only.
- Skip information items even when wrapped in a "Beslut" heading: items whose
  decision text is only "Informationen noteras" (e.g. Meddelanden, information
  från föredragande) and items with no Beslut heading at all (e.g. anmälan av
  frågor/interpellationer when none were submitted).
- Also skip procedural closures with no substantive outcome: "Frågestunden
  anses avslutad" (allmänhetens frågestund with no questions) and "Anmälan tas
  till protokollet" (anmälan av handlingar).
- Include "anses besvarad"/"anses besvarat" outcomes: enkla frågor and
  e-förslag responses are recorded decisions.
- Anmälan av motioner (§): "Motionerna remitteras till kommunstyrelsen" is a
  decision — include.
- Valärenden (§ "Vissa val m.m.") with many numbered items: keep as ONE
  decision entry with full text of all items.
- Beslut som godkänner synpunkter + överlämnar skrivelse (revisionssvar): keep
  both parts in one entry.
- Voting: protocols record "Ordföranden finner att det endast finns ett förslag
  ... beslutar i enlighet med förslaget" without omröstning; omit voting_method
  unless an explicit vote result is printed (e.g. motionsvotering with JA/NEJ/
  AVSTÅR counts — then record voting_method and keep counts in full_text).
- Politicians: use the "Beslutande" list; mark Ordförande / vice ordförande /
  tjänstgörande ersättare as roles. When header names a chair (Ordförande) who
  in the Beslutande list is annotated with their permanent vice-chair role, use
  the header's meeting role.
