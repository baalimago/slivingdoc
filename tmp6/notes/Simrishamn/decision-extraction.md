# Simrishamn KF decision extraction guidance

- KF protocols (PDF) use a "KOMMUNFULLMÄKTIGES BESLUT" block per §; keep all bullets
  of a § in one decision entry, paragraph_number as "§ N" matching the ärendelista
  (ärendelista may repeat "§ 200" for several meddelanden items).
- Include procedural opening items (§ Val av justerare, § Fastställande av ärendelista) —
  they record explicit decision outcomes.
- Skip §-items that are information (e.g. "Information från Sysav"), announcements
  (§ Meddelanden), and the closing § Julhälsning (thanks only, no decision).
- Politicians: "Beslutande" roster on page 2; tjänstgörande ersättare get role
  "Tjänstgörande ersättare" with identifiable tag "tjänstgör för <name> (Party)";
  remaining roster entries are Ledamot. "Övriga deltagande" are not decision-makers; skip.
- Counted voteringar print omröstningsbilagor with tallies inside the §; put the tally
  plus what each side supported in voting_method. Chair-led propositions without a
  recorded vote: omit voting_method.
- Retrieval mechanics for simrishamn.se live in /tmp/sakfraga-notebook/notes/simrishamn.se/retrieval.md.
- If the extracted text contains ONLY the BankID e-signature page (signer names/dates,
  no protocol body), reject the document — there are no decisions to extract.
