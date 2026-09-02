# Mölndal KF decision extraction guidance

- KF protocols (e.g. 2023-02-22) use the standard SiteVision-export layout:
  each § has a "Beslut" heading; the substantive outcome is the text under it.
  Counted votes (omröstning) are summarized in the § body, with full
  omröstningslistor appended at the end of the protocol (per §).
- Skip §§ whose beslut is only "Informationen antecknas till protokollet"
  (revisionsrapporter, preliminärt bokslut etc.) and "Meddelandena antecknas
  till protokollet" (meddelanden) — information/announcement items.
- Skip interpellation §§: "Interpellationen anses besvarad" and
  "Interpellationen får ställas och besvaras vid <datum>" (debate items).
- Include: all valärenden (bolagsstyrelser, lekmannarevisorer, nämndersättare;
  outcome "Val genomfört"), entlediganden/befrielser, rättelser av tidigare
  beslut, taxebeslut, riktlinjer, omorganisationsbeslut, motionssvar (avslås/
  bifalls) and motioner som remitteras.
- "Uppdraget lämnas vakant tills vidare" (fyllnadsval) is a real decision —
  include it.
- voting_method: set only for explicit counted omröstning (with result); plain
  chair-led propositions get no voting_method.
- Politicians: "Beslutande" ledamöter (role Ledamot/Ordförande) plus
  "Ersättare" (role Tjänstgörande ersättare). Övriga närvarande (sekreterare,
  revisorer) are not decision-makers — skip. Note: an ersättare who serves as
  ledamot for part of the meeting (e.g. Ingvar Paulsson, M) appears in both
  lists; include once.
