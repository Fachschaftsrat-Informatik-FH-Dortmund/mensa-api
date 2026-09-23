# mensa-api

Ein kleiner Caching-Server vor der Stwdo-Speiseplan-API: er holt die Daten
periodisch im Hintergrund, wirft die für eine Anzeige unbrauchbaren Felder weg
und liefert eine aufgeräumte JSON-API aus. Analyse der Original-API:
[`upstream/api.md`](./upstream/api.md).

Ein einzelnes Go-Binary ohne Abhängigkeiten außerhalb der Standardbibliothek —
gedacht, um es auf einen Server zu kopieren und dort laufen zu lassen.

## Starten

```bash
pixi run run
```

Dann auf <http://localhost:8080>. Der Port geht sofort auf: geladen wird erst,
wenn eine Anfrage die Daten braucht. Die erste Anfrage auf einen leeren Cache
dauert daher rund eine Sekunde, jede weitere wenige Millisekunden.

```bash
curl -s localhost:8080/canteens/341/menu/today | head -40
```

Für den Server:

```bash
pixi run build && ./bin/mensa-api -addr :8080
```

Das Binary ist statisch gelinkt und braucht auf dem Zielsystem weder pixi noch
Go — nur die Datei.

### Optionen

| Flag            | Default | Bedeutung                                                                                |
| --------------- | ------- | ---------------------------------------------------------------------------------------- |
| `-addr`         | `:8080` | Adresse, auf der gelauscht wird                                                          |
| `-ttl`          | `1h`    | wie lange Speisepläne und Öffnungszeiten im Cache frisch bleiben                         |
| `-canteens-ttl` | `24h`   | wie lange die Mensa-Liste (inkl. Adressen und Namen aus dem CMS) frisch bleibt           |
| `-timeout`      | `20s`   | Timeout pro Anfrage an stwdo.de                                                          |
| `-delay`        | `200ms` | Pause zwischen zwei aufeinanderfolgenden Anfragen an stwdo.de                            |
| `-warm`         | `false` | beim Start laden statt bei der ersten Anfrage                                            |
| `-docs`         | `true`  | `/docs` ausliefern; `-docs=false` schaltet nur die HTML-Seite ab, `/openapi.json` bleibt |

## Endpunkte

| Pfad                             | Was zurückkommt                                                                        |
| -------------------------------- | -------------------------------------------------------------------------------------- |
| `GET /canteens`                  | alle Mensen mit Name, Adresse, Maps-Link, Beschreibung                                 |
| `GET /canteens/{id}`             | eine Mensa                                                                             |
| `GET /canteens/{id}/menu`        | die nächsten zwei Wochen, nach Tag und Kategorie gruppiert                             |
| `GET /canteens/{id}/menu/{date}` | ein Tag; `date` ist `YYYY-MM-DD` oder `today`                                          |
| `GET /canteens/{id}/hours`       | heute, 7-Tage-Vorschau, Wochenplan, Schließtage                                        |
| `GET /legend`                    | Zusatzstoffe, Allergene, Kennzeichen und CO₂-Klassen im Klartext, deutsch und englisch |
| `GET /health`                    | Alter jeder Cache-Gruppe, Anzahl Mensen und Gerichte, Fehler                           |
| `GET /openapi.json`              | die Spezifikation dieser API (OpenAPI 3.1)                                             |
| `GET /docs`                      | die Spezifikation gerendert, siehe unten                                               |

Beispiel für ein Gericht:

```json
{
  "id": 2889754,
  "name": "Rigatonigratin",
  "nameEn": "Rigatoni gratin",
  "lines": ["Rigatonigratin", "Salat", "Vinaigrette"],
  "linesEn": ["Rigatoni gratin", "salad", "vinaigrette"],
  "prices": { "student": 3.3, "staff": 5.4, "guest": 6.5 },
  "tags": ["beef", "animal-welfare"],
  "additives": ["2", "20", "20a", "25", "26", "28", "29"],
  "co2Class": "B"
}
```

## Was der Server anders macht als das Original

- **Cache statt Durchreichen.** Anfragen erreichen stwdo.de nie. Geladen wird
  bedarfsgesteuert: Eine Cache-Gruppe wird erst dann neu geholt, wenn eine
  Anfrage sie braucht _und_ sie abgelaufen ist. Es läuft kein Timer im
  Hintergrund — ein Dienst, den niemand fragt, erzeugt bei stwdo.de gar keine
  Last, und die Anfragen, die er stellt, folgen echtem Verkehr statt einer Uhr.
  Schlägt ein Abruf fehl, bleibt der alte Stand stehen, statt eine Lücke zu
  liefern.
- **Niemand wartet auf einen warmen Cache.** Ist eine Gruppe abgelaufen, bekommt
  der Client sofort den etwas älteren Stand, und der Refresh läuft hinter seinem
  Rücken (stale-while-revalidate). Nur ein komplett leerer Cache lässt warten.
  Treffen dabei viele Anfragen gleichzeitig ein, holt trotzdem genau eine die
  Daten.
- **Ein Sweep statt 31 Anfragen.** Der `verbrauchsortnr`-Filter der Artikel-API
  ist optional; ohne ihn kommen die Speisepläne aller Mensen zusammen — aktuell
  1066 Gerichte in vier Seiten statt dreizehn Einzelabrufen. Nur die
  Öffnungszeiten bleiben eine Anfrage pro Mensa, weil der Upstream dort auf
  `restaurant_id` besteht; die werden deshalb einzeln und nur für tatsächlich
  abgefragte Mensen geladen.
- **45 Felder werden 10.** Die zehn dauerhaft leeren Nährwertfelder, die
  Küchennotizen, der Kassenindex und der Monitor-Slot fallen weg. Die sieben
  `AUSGABETEXTZEILE*` werden ein `lines`-Array, die Allergencodes wandern aus
  dem Fließtext in ein eigenes Feld.
- **CORS.** Das Original sendet keinen einzigen CORS-Header, deshalb kommt kein
  Browser von einer fremden Domain an die Daten. Dieser Server erlaubt jede
  Origin.
- **Vollständige Speisepläne.** Das Original deckelt `limit` still bei 300; die
  Galerie hat in zwei Wochen aber 385 Gerichte. Hier wird über `skip`
  durchgeblättert, bis nichts mehr kommt — die Website selbst zeigt die letzten
  Tage dieser Mensa gar nicht an.
- **Vollständige Mensa-Liste.** `/verbrauchsorte` kennt drei Standorte nicht,
  die die Website führt (453 Max-Ophüls-Platz, 467 und 468 Café C). Die Liste wird
  deshalb mit den CMS-Seiten zusammengeführt — daher kommen auch die
  brauchbaren Namen (`Hauptmensa` statt `Hauptmensa inklKalteKüche`) und die
  Adressen.
- **ISO-Datumsangaben** statt `TT.MM.JJJJ`, Uhrzeiten ohne die sinnlosen
  Sekunden.

## API-Referenz

<http://localhost:8080/docs> rendert [`openapi.json`](./openapi.json) mit
[Swagger UI](https://swagger.io/tools/swagger-ui/). Die Spezifikation ist
handgeschrieben und per `go:embed` im Binary, das UI kommt vom CDN — es liegt
also weder im Repo noch im Binary, und `-docs=false` lässt nichts davon zurück.
Die einzige Folge: ohne Internet im Browser bleibt `/docs` leer,
`/openapi.json` funktioniert weiter.

Geladen wird nur `swagger-ui-bundle`, nicht das Standalone-Preset: Letzteres
bringt die Topbar mit dem URL-Eingabefeld mit, die bei einem festen Dokument
nichts nützt. Das CDN-Script ist auf die Major-Version gepinnt
(`swagger-ui-dist@5`).

Die Seite deklariert sich per `<meta name="color-scheme" content="light">` als
hell. Swagger UI bringt kein eigenes Dark Theme mit; ein Browser im Dark Mode
würde die Seite sonst algorithmisch invertieren, was die Farben zerlegt. Eigene
CSS-Regeln gibt es keine.

Weil die Spezifikation von Hand gepflegt wird, prüft
[`docs_test.go`](./docs_test.go), dass sie gültiges JSON ist und dass Routen
und dokumentierte Pfade sich exakt decken — in beide Richtungen.

## Deployment

Gehostet auf <https://mensa.fb4.it>. Jeder Push auf `main` baut das Binary und
schiebt es auf den Server:
[`.github/workflows/deploy.yml`](./.github/workflows/deploy.yml). Der Workflow
baut mit der in `pixi.lock` festgenagelten Go-Version, kopiert die eine Datei
per rsync nach `/opt/mensa-api/` und startet den Dienst neu. Danach pollt er
`/health`, bis der Dienst antwortet — schlägt das fehl, schlägt der Workflow
fehl. Dass der Cache dabei noch leer ist (`"status": "cold"`), ist kein Fehler,
sondern der Normalfall.

Auf dem Server läuft mensa-api als systemd-Dienst auf `127.0.0.1:8080`, nginx
terminiert TLS und leitet `mensa.fb4.it` dorthin weiter. Die systemd-Unit, der
nginx-vhost, die sudoers-Zeile für den Deploy-Benutzer und die Anleitung zur
einmaligen Einrichtung liegen **nicht hier** — sie nennen Benutzer und Pfade der
Maschine, und dieses Repository ist öffentlich. Wer sie braucht, fragt im FSR.

Der Dienst läuft unter `DynamicUser=yes` — es gibt keinen Zustand auf der
Platte, der Cache liegt nur im Speicher. Nach einem Neustart ist er leer und
füllt sich mit den ersten Anfragen wieder.

Auf dem Server läuft er mit `-docs=false`: die Swagger-UI-Seite gibt es dort
nicht, <https://mensa.fb4.it/docs> antwortet mit 404.
[`openapi.json`](./openapi.json) wird weiterhin ausgeliefert — die Spezifikation
nützt Client-Generatoren, die HTML-Seite mit ihrem CDN-Script nicht. Lokal ist
`/docs` unverändert da.

Die drei Secrets `SSH_HOST`, `SSH_USERNAME` und `SSH_PRIVATE_KEY` müssen im
Repository gesetzt sein; `SSH_PORT` ist optional und fällt auf 22 zurück.

## Aufbau

| Datei                              | Inhalt                                                           |
| ---------------------------------- | ---------------------------------------------------------------- |
| [`main.go`](./main.go)             | Flags, Routing, JSON- und Logging-Helfer                         |
| [`upstream.go`](./upstream.go)     | HTTP-Client für stwdo.de, Rohtypen                               |
| [`model.go`](./model.go)           | die ausgelieferten Typen und die Umwandlung                      |
| [`cache.go`](./cache.go)           | Speicher, bedarfsgesteuerter Refresh, Zusammenführung der Listen |
| [`legend.go`](./legend.go)         | statische Legenden (Allergene, Kennzeichen)                      |
| [`docs.go`](./docs.go)             | `/openapi.json` und die Swagger-UI-Seite                         |
| [`openapi.json`](./openapi.json)   | Spezifikation dieser API, handgeschrieben                        |
| [`model_test.go`](./model_test.go) | Tests für die Umwandlung                                         |
| [`cache_test.go`](./cache_test.go) | Tests für Auffrischung, Stampede-Schutz und Jitter               |
| [`docs_test.go`](./docs_test.go)   | Tests gegen veraltete Spezifikation                              |
| [`upstream/`](./upstream/)         | die Recherche zur fremden API, die dieser Server kapselt         |

Zwei Dateien heißen `openapi.json`: die hier oben ist die Spezifikation
**dieses** Servers, [`upstream/openapi.json`](./upstream/openapi.json) die des
Studierendenwerks.

Die Filterung passiert an zwei Stellen: `rawMeal` in `upstream.go` deklariert
nur die Felder, die uns interessieren — alles andere verwirft der JSON-Decoder
von selbst —, und `toMeal` in `model.go` baut daraus die Ausgabestruktur.

```bash
pixi run test
```

## Bekannte Eigenheiten der Quelle

Drei Dinge, über die der Code stolpert und die deshalb behandelt sind:

- `restaurant_id` kommt aus dem CMS mal als Zahl (`341`), mal als String
  (`"341"`) — siehe `flexInt` in [`upstream.go`](./upstream.go).
- Eine CMS-Seite kann mehrere `mensa_hero`-Blöcke haben: Café C liegt an zwei
  Standorten und ist deshalb zwei Mensen (467 und 468) auf einer Seite.
- `FREIKENNZEICHEN` trennt meist mit Komma, in einzelnen Datensätzen aber mit
  Semikolon.

Weitere Fallstricke stehen in
[`upstream/api.md`](./upstream/api.md).

## Was fehlt

Der Cache liegt nur im Speicher; nach einem Neustart ist er leer. Der Port geht
inzwischen sofort auf, ein Deploy kostet also keine 502er mehr — nur die ersten
Anfragen danach dauern rund eine Sekunde statt Millisekunden.

Überwacht wird nichts: `/health` fragt nur der Deploy-Workflow ab, und wenn der
Dienst nachts stirbt, startet ihn systemd zwar neu, sagt aber niemandem
Bescheid.
