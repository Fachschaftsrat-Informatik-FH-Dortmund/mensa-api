# Stwdo Speiseplan-API

Analyse der API hinter <https://www.stwdo.de/mensa-cafes-und-catering> — der
fremden API, vor der [dieser Server](../README.md) sitzt, und damit die Grundlage
für die Hälfte der Design-Entscheidungen im Code.
Stand der Erhebung: **2026-09-09**, API-Version `0.7.4`, OpenAPI-Dokument `1.0.0`.

Die Quelle hat ein ordentliches OpenAPI-Dokument:
**<https://www.stwdo.de/api/v2/speiseplan/openapi.json>** — Kopie vom 2026-09-09
in [`openapi.json`](./openapi.json), nur zum Lesen eingerückt. **Endpunkte,
Query-Parameter mit Defaults, Feldnamen, Typen, Nullability und das komplette
Öffnungszeiten-Modell stehen dort und werden hier nicht wiederholt.**

Hier steht nur, was das Schema nicht hergibt: die _Bedeutung_ der Artikelfelder
(die `title`-Angaben dort sind nur automatisch aus den Feldnamen erzeugt,
`"Vkpreisstud"`, und Beschreibungen fehlen dem `MenuItemRawSchema` ganz), wie gut
die Felder tatsächlich belegt sind, die Wertelegenden und die beobachteten
Abweichungen zwischen Doku und Live-Verhalten.

> **Nicht verwechseln:** [`openapi.json`](./openapi.json) in diesem Ordner
> beschreibt die API des Studierendenwerks. Die Spezifikation _unseres_ Servers
> liegt eine Ebene höher in [`../openapi.json`](../openapi.json).

## Grundlagen

|           |                                                                                                               |
| --------- | ------------------------------------------------------------------------------------------------------------- |
| Basis-URL | `https://www.stwdo.de/api/v2/speiseplan` — das Schema hat einen leeren `servers`-Eintrag, der Host fehlt dort |
| CORS      | **kein** `Access-Control-Allow-Origin`-Header → nicht direkt aus fremden Browser-Origins nutzbar, Proxy nötig |
| Antwort   | nacktes JSON-Array ohne Envelope, insbesondere ohne Gesamtzahl für das Blättern mit `skip`                    |
| Technik   | FastAPI/Django-Ninja hinter nginx; daneben liegt eine Wagtail-API unter `/api/v2/pages/`                      |

## Verbrauchsorte (Stand 2026-09-09)

`GET /verbrauchsorte` liefert 12 Einträge. Die letzte Spalte ist die zugehörige
Website-Seite, sie stammt **nicht** aus dieser API (siehe „Wagtail-API“ unten).

| id  | name (API)                | Seite unter `/mensa-cafes-und-catering/` |
| --- | ------------------------- | ---------------------------------------- |
| 341 | Hauptmensa inklKalteKüche | `hauptmensa`                             |
| 342 | Mensa Süd                 | `mensa-süd`                              |
| 451 | Galerie                   | `galerie`                                |
| 452 | Archeteria                | `archeteria`                             |
| 455 | Mensa Sonnenstraße        | `mensa-sonnenstraße`                     |
| 456 | kostBar                   | `mensa-kostbar`                          |
| 470 | Mensa Snack it            | `mensa-snack-it`                         |
| 472 | Mensa Canape              | `canapé`                                 |
| 474 | food fakultät             | `food-fakultät`                          |
| 480 | Mensa da Vinci            | `mensa-da-vinci`                         |
| 482 | Mensa Soest               | `mensa-soest`                            |
| 800 | Kindertagesstätte         | — (keine eigene Mensa-Seite)             |

Drei weitere IDs sind auf der Website hinterlegt, fehlen aber in
`/verbrauchsorte`: **453** (Mensa Max-Ophüls-Platz) sowie **467** und **468**
(Café C, zwei Standorte auf einer einzigen CMS-Seite — die Seite enthält zwei
`mensa_hero`-Blöcke). Öffnungszeiten liefern alle drei, der Speiseplan ist leer
(`[]`). Ein Client darf `/verbrauchsorte` also nicht als vollständige
Restaurantliste behandeln, und beim Auswerten der CMS-Seiten muss er mit
mehreren Hero-Blöcken pro Seite rechnen.

## Bedeutung der Artikelfelder (`MenuItemRawSchema`)

Die Belegungsquote unten wurde über 1003 Artikel aus allen 12 Verbrauchsorten
gemessen (je Ort bis zum `limit` von 300, Zeitraum 09.09.–23.09.2026) — bei
„Galerie“ und „Archeteria“ ist die Stichprobe also abgeschnitten.

### Grundinfos — für eine minimale Anzeige

| Feld                                   | belegt                      | Bedeutung                                                                                                                                                                                                                      |
| -------------------------------------- | --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `PRODUKTIONSNUMMER`                    | 100 %                       | eindeutige ID, Schlüssel für `/artikel/{id}`, geeignet als React-/Vue-Key                                                                                                                                                      |
| `PRODUKTIONSDATUM`                     | 100 %                       | Tag, an dem das Gericht angeboten wird                                                                                                                                                                                         |
| `VERBRAUCHSORTNR` / `VERBRAUCHSORT`    | 100 %                       | Ort der **Ausgabe** — das ist der Filterwert `verbrauchsortnr`                                                                                                                                                                 |
| `PRODUKTIONSARTNR` / `PRODUKTIONSNAME` | 100 %                       | Kategorie/Ausgabestelle, z. B. `101` = „Menü 1 Mensa“; ideal zur Gruppierung im UI                                                                                                                                             |
| `AUSGABETEXTZEILE1`…`7`                | Z1 100 %, Z2 54 %, … Z7 7 % | **die deutsche Gerichtsbeschreibung**, eine Komponente pro Zeile (Hauptkomponente, Beilage, Sauce, Salat …). Zeilen von 1 an aufsteigend lesen und leere überspringen; die Allergen-Codes stehen als `(20a,26)` direkt im Text |
| `VKPREISSTUD`                          | 100 %                       | Preis Studierende in Euro — der Preis, den man anzeigt                                                                                                                                                                         |
| `FREIKENNZEICHEN`                      | 64 %                        | Ernährungs-/Herkunftskennzeichen als Codeliste, siehe Legende                                                                                                                                                                  |

Ein minimaler Speiseplan braucht damit: `/naechste-2-wochen?verbrauchsortnr=<id>`,
nach `PRODUKTIONSDATUM` filtern, nach `PRODUKTIONSARTNR` gruppieren, pro Artikel
die `AUSGABETEXTZEILE*` zusammensetzen, `VKPREISSTUD` und die Icons aus
`FREIKENNZEICHEN` dazu.

### Zusatzinfos — optional, für Detailansicht und Filter

| Feld                                      | belegt                  | Bedeutung                                                                                                                                                          |
| ----------------------------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `PRODUKTIONSBEZEICHNUNG`                  | 100 %                   | interner Kurzname („Rigatonigratin Rind“). Küchen-Sprache, nicht die Gästebeschreibung — für die Anzeige `AUSGABETEXTZEILE*` nehmen                                |
| `PRODUKTIONSORTNR` / `PRODUKTIONSORTNAME` | 100 %                   | Ort der **Zubereitung**. Weicht in 4 Fällen vom Verbrauchsort ab (Hauptmensa kocht für da Vinci, Mensa Süd für Archeteria und Kita, Canape für Snack it)           |
| `AUSGABETEXT2ZEILE1`…`8`                  | Z1 98 %, danach fallend | englische Übersetzung der Beschreibung                                                                                                                             |
| `VKPREISBED`                              | 100 %                   | Preis Bedienstete                                                                                                                                                  |
| `VKPREISGAST`                             | 100 %                   | Preis Gäste                                                                                                                                                        |
| `VKPREISSCHUL`                            | 100 %                   | Preis Schüler; faktisch nur `0.00` (93 %) oder `0.50` (7 %) — als „nicht gepflegt“ behandeln                                                                       |
| `ZUSATZSTOFFNUMMERN`                      | 88 %                    | vollständige Zusatzstoff-/Allergenliste des Gerichts, kommasepariert (`"2, 20, 20a, 25"`), redundant zu den Codes in den Ausgabetexten, aber maschinell auswertbar |
| `KLIMATELLER`                             | 99 %                    | CO₂-Klasse `A`/`B`/`C`/`E`, siehe Legende                                                                                                                          |
| `HINWEISE`                                | 3 %                     | interne Küchennotiz („12 Kelle“, „Gurkensalat mit auf dem Teller anrichten“) — **nicht für Gäste anzeigen**                                                        |
| `ZUSATZBEZEICHNUNG`                       | 1 %                     | Zusatztitel, im Beobachtungszeitraum nur ein einziger Wert                                                                                                         |
| `MONITORSCHALTER`                         | 59 %                    | Slot/Reihenfolge auf den Ausgabe-Monitoren (1–4, 10, 15, 20). Vom offiziellen Frontend nicht ausgewertet                                                           |
| `KASSENINDEX`                             | 72 %                    | Kassenplatz-Index, rein betrieblich                                                                                                                                |

### Nicht belegt

`NAEHRWERTEJE100G`, `NAEHRWERTEJEPORT`, `NW_KJ`, `NW_KCAL`, `NW_FETT`,
`NW_GESFETT`, `NW_EIWEISS`, `NW_KH`, `NW_ZUCKER`, `NW_SALZ`

Alle 10 Nährwertfelder waren in **allen 1003** geprüften Artikeln `null`. Das
offizielle Frontend liest `NW_KCAL`, `NW_FETT`, `NW_EIWEISS` und `NW_KH` und
blendet den Nährwertblock aus, wenn alle leer sind — praktisch also immer.
Ein Client sollte sich nicht darauf verlassen, dass hier je Daten kommen, den
Fall aber tolerieren.

## Legenden

Keine davon steht im Schema; die Felder sind dort schlichte Strings.

### `FREIKENNZEICHEN`

Codeliste, Trennzeichen ist **Komma oder Semikolon** (beides kommt in den Daten
vor: `"S,A,B"`, aber auch `"N;B"`). Nach dem Trennen trimmen und
großschreiben.

| Code | Bedeutung                                                                                                                 |
| ---- | ------------------------------------------------------------------------------------------------------------------------- |
| `V`  | Vegetarisch                                                                                                               |
| `N`  | Vegan                                                                                                                     |
| `G`  | Geflügel                                                                                                                  |
| `S`  | Schwein                                                                                                                   |
| `R`  | Rind                                                                                                                      |
| `L`  | Lamm                                                                                                                      |
| `W`  | Wild                                                                                                                      |
| `F`  | Fisch                                                                                                                     |
| `A`  | Artgerecht                                                                                                                |
| `B`  | **nicht in der Legende des Frontends**; kommt in den Daten häufig vor (231 ×) und wird vom offiziellen Frontend ignoriert |

`L` und `W` sind im Frontend definiert, kamen im Beobachtungszeitraum aber nicht
vor. Ein leeres `FREIKENNZEICHEN` (36 % der Artikel) heißt nur „keine
Kennzeichnung“, nicht „enthält nichts davon“.

### `KLIMATELLER`

Werte `A` (46 %), `B` (30 %), `E` (20 %), `C` (3 %), leer (0,5 %) — eine
CO₂-Klassifizierung. Das offizielle Frontend behandelt sie inkonsistent:

- Das Klimateller-Icon wird **nur bei `A`** gesetzt.
- Der Filter „Klimateller“ trifft dagegen **jeden nicht-leeren Wert**.

Für eigene Clients ist `A` = Klimateller die brauchbarere Auslegung.

### `ZUSATZSTOFFNUMMERN` / Codes in den Ausgabetexten

Die Legende kommt **nicht** aus dieser API, sondern von der Website
(<https://www.stwdo.de/mensa-cafes-und-catering/allgemein/zusatzstoffe/>, per
Wagtail-API unter `/api/v2/pages/164/`). Stand 2026-09-09:

**Zusatzstoffe:** `1` Antioxidationsmittel · `2` Konservierungsstoff ·
`3` geschwefelt · `4` Farbstoff · `5` gewachst · `6` Geschmacksverstärker ·
`7` Süßungsmittel · `8` enthält eine Phenylalaninquelle · `9` Phosphat ·
`10` geschwärzt · `11` Alkohol

**Allergene** (jeweils „und Erzeugnisse daraus“): `20a` Gluten aus Weizen ·
`20b` Roggen · `20c` Gerste · `20d` Hafer · `20e` Dinkel · `20f` Kamut ·
`21` Krebstiere · `22` Eier · `23` Fisch · `24` Erdnüsse · `25` Soja ·
`26` Milch inkl. Lactose · `27a` Mandeln · `27b` Haselnüsse · `27c` Walnüsse ·
`27d` Kaschunüsse · `27e` Pekannüsse · `27f` Paranüsse · `27g` Pistazien ·
`27h` Macadamia-/Queenslandnüsse · `28` Sellerie · `29` Senf · `30` Sesamsamen ·
`31` Schwefeldioxid/Sulfite > 10 mg/kg · `32` Lupine · `33` Weichtiere

In den Daten kam zusätzlich der Code `20` (ohne Buchstabe) vor, der in der
Legende fehlt — vermutlich „Gluten, nicht näher bestimmt“. Ein Parser sollte
unbekannte Codes unverändert durchreichen statt zu verwerfen.

### `PRODUKTIONSARTNR`

Die 20 Kategorien holt man vollständig über `GET /kategorien`. Wissenswert ist
nur die Ausnahme: Kategorie **116 („Beilagen Speiseplan“)** ist im offiziellen
Frontend ein Sonderfall und wird anders gerendert als ein vollwertiges Gericht.

## Wagtail-API (Drumherum zur Mensa)

Adresse, Bild, Beschreibungstext und Google-Maps-Link einer Mensa liegen nicht in
der Speiseplan-API, sondern in der Wagtail-CMS-API:

- `GET https://www.stwdo.de/api/v2/pages/?limit=150` — `limit` max. 150.
  Mensa-Seiten haben `meta.type == "home.MensaPage"`.
- `GET https://www.stwdo.de/api/v2/pages/54/` — Hauptmensa. Relevante Blöcke in `intro[]`:
  - `mensa_hero` (kann mehrfach vorkommen) → `title`, `subtitle`, `location` (Postadresse), `text`,
    `google_maps_url`, `image` (Bild-ID), sowie `opening_times.restaurant_id`
    und ein **eingebettetes Abbild der Öffnungszeiten-Antwort** unter
    `opening_times.data`
  - `speiseplan` → `restaurant_id` + `headline`
- `GET https://www.stwdo.de/api/v2/pages/164/` — Legende Zusatzstoffe/Allergene als HTML.

Die IDs 54–66 sind die 13 Mensa-/Cafeteria-Seiten.

## Beobachtete Abweichungen und Fallstricke

1. **`/artikel` ohne Filter liefert Vergangenheit.** Die Doku sagt „ohne Filter
   … standardmäßig die Artikel der nächsten zwei Wochen“; tatsächlich kamen am
   09.09.2026 Artikel vom 04.–08.09. Für „ab heute“ explizit
   `naechste_2_wochen=true` setzen oder `/naechste-2-wochen` nehmen.
2. **Ein falsches `datum`-Format fällt nicht auf.** `datum=2026-09-10` liefert
   `[]` mit Status 200 statt eines Fehlers.
3. **Kein CORS.** Direkter `fetch` aus einer fremden Origin scheitert; die
   Website selbst ruft same-origin auf.
4. **`/verbrauchsorte` ist unvollständig** (453, 467, 468 fehlen, siehe oben).
5. **Nährwerte sind durchgehend leer**, obwohl 10 Felder dafür existieren.
6. Ein Ort kann pro Tag mehrere Artikel derselben Kategorie haben — die
   Gruppierung nach `PRODUKTIONSARTNR` muss n Artikel je Gruppe vertragen.

## Aktualisieren

```bash
curl -s https://www.stwdo.de/api/v2/speiseplan/openapi.json | python3 -m json.tool --no-ensure-ascii > openapi.json
```

Danach prüfen, ob dieses Dokument noch stimmt — die Belegungsquoten und Legenden
hier sind Momentaufnahmen vom 2026-09-09 und stehen so nicht im Schema.
