# Eksjö KF decision extraction guidance

- Eksjö KF protocols use a "Beslut" block per paragraph headed "Kommunfullmäktige
  beslutar att ...".
- Skip paragraphs whose beslut only says "notera informationen" or "anmäls":
  information points (t.ex. Kommunrevisionen informerar, information från helägda
  bolag, årsredovisning-information), anmälan av nämnd- och styrelseprotokoll, and
  Länsstyrelsebeslut om ny/ersättare that are only noted. Also skip paragraphs with
  no Beslut block (t.ex. Allmänhetens frågestund).
- Keep remittering of medborgarförslag (to nämnd/kommunstyrelsen with
  återredovisning date) as decisions.
- Keep årsredovisning/ansvarsfrihet paragraphs (bolagskoncern, delägda bolag) with
  all numbered att-clauses preserved in full_text; jäv-noteringar (X deltar inte i
  beslutet på grund av jäv) are part of the beslut record and can stay in full_text.
- Keep avsägelse av uppdrag paragraphs: godkänna avsägelsen + hemställan om ny
  sammanräkning/överlämnande till valberedningen.
- Voting: omit voting_method unless an explicit omröstning is recorded; Eksjö KF
  beslut list only "Yrkande (bifall)" without counts.
