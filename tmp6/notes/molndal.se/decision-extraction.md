# Mölndal KF decision extraction (2026 protocols)

- Each § has a "Beslut" heading. Not every Beslut is substantive:
  - "Meddelanden" (§ Meddelanden) and "Information om revisionsrapport ..." items
    decide only to "anteckna ... till protokollet" / "lägga ... till handlingarna" —
    announcements/information items, skip (even though wrapped in Beslut).
  - Items whose Beslut is note-taking of submitted documents (e.g. nämndernas
    verksamhetsplaner "antecknas till protokollet") are information-type, skip.
  - Items marked "Ärendet utgår" (withdrawn) have no Beslut text — skip.
- Substantive decisions to extract: befrielse från uppdrag (with Länsstyrelsen
  tillskrivning), val (lekmannarevisorer, styrelser, suppleanter, ersättare),
  uppdrag till bolag, antagande av ägardirektiv, avslag på motion (keep the
  majority's motivering in full_text; note reservationer in summary), remittering
  av motioner, interpellationssvar ("anses besvarad"), and
  "Interpellationen får ställas och bör besvaras vid nästa fullmäktigesammanträde".
- Jäv notes (large blocks of names "tjänstgör ej § 43 på grund av jäv") are
  participation notes, not decision content; mention jäv in the § summary only.
- No omröstning in these protocols; propositions are decided by acclamation
  ("Ordföranden finner att så sker") — omit voting_method.
