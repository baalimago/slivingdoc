# bastad.se scanner notes

- The scoped target domain (bastad.se) may not match the municipality named in
  the document. In one run the protocol was from Borlänge kommun
  (Kommunfullmäktige 2024-12-10). Trust the document text for
  organization_name/location; do not reject or rename based on the target
  directory (same guidance as bollnas.se note).
- In Kommunfullmäktige protocols, interpellation/ärenden items whose Beslut is
  "Interpellationen med svaret läggs till handlingarna" and "Frågan besvaras
  och läggs till handlingarna" were accepted as explicit decision outcomes.
- "Ärendet utgår" recorded under Beslut (e.g. Valärenden) was accepted as a
  decision outcome (item withdrawn), not skipped.
- Items without a Beslut block (e.g. § Utdelning av gåvor/avslutningstal) are
  information items; skip them.
