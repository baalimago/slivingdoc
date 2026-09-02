# Sorsele KF decision extraction

- Sorsele Kommunfullmäktige protocols (Addo Sign template) use a
  "Kommunfullmäktige beslutar" block per §.
- Extract procedural approval paragraphs too when they carry an explicit
  beslutsblock: "Godkännande av kallelsen" (godkänns), "Fastställande av
  ärendelistan" (fastställs).
- Skip items without a beslutsblock ("Allmänhetens frågestund" with no
  incoming questions, "Ordförandena informerar") and noting/filing items:
  "Information ..." (informationen noteras), "Meddelanden" (läggs till
  handlingarna), "Inkomna medborgarförslag och motioner" (none received).
- Keep "Redovisning av ... under beredning" when wording is "godkänner
  redovisningen" (approval, not just tagit del av).
- Budget paragraphs print a reservation under the beslut ("Reserverar sig mot
  beslutet till förmån för eget förslag") - include in full_text/outcome.
- Budget beslutsgång may mention a vote on återremiss without counts; omit
  voting_method when no counts are printed.
- Decisions are by acclamation unless a votering with counts is printed.
- Party codes to expect: V, S, M, C.
