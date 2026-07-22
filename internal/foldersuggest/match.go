package foldersuggest

import (
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MinScore is the score a folder must reach before it is offered at all.
// Below it, the suggestion is withheld: an empty field the user fills in
// themselves costs a few seconds, a wrong path silently sends a course's
// downloads somewhere they will never look for them.
const MinScore = 0.75

// MinMargin is how far ahead of the also-rans outside the tie band the
// winner must be. A folder that is merely somewhat plausible must not be
// beaten narrowly and then offered anyway.
const MinMargin = 0.08

// TieBand is how close to the leader a folder has to be before the name is
// treated as having failed to decide, handing the choice to the structural
// tie-breaks in bestCandidate. Set from measurement rather than taste: on a
// real tree, last semester's "Ma-Programmieren" outscored this semester's
// "Ma-Prog" by 0.087 and was confidently suggested. Names alone cannot tell
// those apart, and pretending otherwise is what produced the one wrong
// answer that mattered.
const TieBand = 0.10

// MinAgeGap is how much newer a folder must be before recency is allowed to
// break a tie. Two folders both in use this month are a real ambiguity and
// must stay one; a folder untouched since last semester is not a rival.
const MinAgeGap = 14 * 24 * time.Hour

// diacriticFolder maps the accented letters that actually turn up in German
// course titles onto their ASCII skeletons, so "Übungen"/"Ubungen" and
// "Weiterführende"/"Weiterfuhrende" compare equal. Applied after
// lowercasing, so only the lowercase forms need entries.
var diacriticFolder = strings.NewReplacer(
	"ä", "a", "ö", "o", "ü", "u", "ß", "ss",
	"á", "a", "à", "a", "â", "a", "ã", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ø", "o",
	"ú", "u", "ù", "u", "û", "u",
	"ñ", "n", "ç", "c", "ý", "y",
)

func fold(s string) string {
	return diacriticFolder.Replace(strings.ToLower(s))
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// courseStopWords are grammatical filler that carries no matching signal.
// Dropping them matters for the acronym rule: "Algorithmen und
// Datenstrukturen" has to reduce to the initials "ad", not "aud".
var courseStopWords = map[string]bool{
	"und": true, "and": true, "oder": true, "or": true,
	"der": true, "die": true, "das": true, "den": true, "dem": true, "des": true,
	"the": true, "of": true, "for": true, "fur": true, "mit": true, "with": true,
	"im": true, "in": true, "zu": true, "zur": true, "zum": true, "von": true,
	"a": true, "an": true, "am": true,
}

// courseNoiseWords are the boilerplate OPAL course titles carry around:
// semester markers, "Modul", and the like. They are dropped so a folder
// named after a semester or a module cannot win on that alone, and so the
// acronym rule sees only the words that name the subject.
var courseNoiseWords = map[string]bool{
	"sose": true, "wise": true, "ss": true, "ws": true, "so": true, "wi": true,
	"sommer": true, "winter": true, "semester": true, "sem": true,
	"modul": true, "module": true, "kurs": true, "kurse": true, "course": true,
}

// splitWords breaks a course title or folder name into comparable lowercase
// words, splitting on punctuation/whitespace and on the case and
// letter/digit boundaries an abbreviation hides its structure in:
// "AlgData" -> [alg data], "Math-Ba-NM20" -> [math ba nm 20],
// "TUDMath" -> [tud math].
func splitWords(s string) []string {
	rs := []rune(s)
	var out []string
	var cur []rune

	flush := func() {
		if len(cur) > 0 {
			out = append(out, fold(string(cur)))
			cur = cur[:0]
		}
	}

	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(cur) > 0 {
			prev := rs[i-1]
			switch {
			case unicode.IsDigit(r) != unicode.IsDigit(prev):
				flush()
			case unicode.IsUpper(r) && unicode.IsLower(prev):
				flush()
			case unicode.IsUpper(prev) && unicode.IsUpper(r) && i+1 < len(rs) && unicode.IsLower(rs[i+1]):
				// Trailing capital of a run that starts the next word:
				// "TUDMath" splits before the M, not before the D.
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()

	return out
}

// semesterMarker matches the run-together semester tags OPAL titles carry
// ("SoSe 2026", "WiSe24/25"). They have to go before splitting rather than
// after: the case boundary inside "SoSe" leaves a bare "se" behind, and a
// noise-word list broad enough to drop that would also drop real two-letter
// course codes like "LA".
var semesterMarker = regexp.MustCompile(`(?i)\b(?:so|wi)se`)

// courseTokens is splitWords over a course title with the filler removed.
//
// Pure digits go too: they are years, semester numbers and module-code
// suffixes ("2026", "LA20"), never the name of a subject. Folder segments
// that are pure digits are ignored rather than treated as misses (see
// segmentsScore), so dropping them here does not turn "Analysis 2" into a
// mismatch.
func courseTokens(name string) []string {
	words := splitWords(semesterMarker.ReplaceAllString(name, " "))
	out := make([]string, 0, len(words))
	for _, w := range words {
		if isNumeric(w) || courseStopWords[w] || courseNoiseWords[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func commonPrefixLen(a, b string) int {
	ar, br := []rune(a), []rune(b)
	n := 0
	for n < len(ar) && n < len(br) && ar[n] == br[n] {
		n++
	}
	return n
}

// Score rates how well folderName reads as an abbreviation of courseName,
// from 0 (nothing in common) to 1 (the same words). It is the whole of the
// matching logic; everything else in this package is plumbing around it.
//
// Three independent readings are tried and the most generous wins, because
// real abbreviations are built in at least three different ways:
//
//   - word-by-word ("Numerische Mathematik" -> "NuMa"),
//   - as an initialism ("LA20" -> "Lineare Algebra"),
//   - by cutting up one German compound ("Softwaretechnologie" -> "SoTech").
func Score(courseName, folderName string) float64 {
	tokens := courseTokens(courseName)
	segments := splitWords(folderName)
	if len(tokens) == 0 || len(segments) == 0 {
		return 0
	}

	best := segmentsScore(segments, tokens)
	if s := acronymScore(segments, tokens); s > best {
		best = s
	}
	if s := compoundScore(segments, tokens); s > best {
		best = s
	}
	return best
}

// segmentsScore reads the folder name word by word: every segment has to be
// explained by some word of the course title. Longer segments count for
// more (a matching "programmieren" says far more than a matching "ma"), and
// a segment nothing explains drags the average down to where the threshold
// rejects the whole candidate.
func segmentsScore(segments, tokens []string) float64 {
	var totalWeight, sum float64
	for _, seg := range segments {
		if isNumeric(seg) {
			continue
		}
		weight := float64(runeLen(seg))
		totalWeight += weight
		sum += weight * segmentScore(seg, tokens)
	}
	if totalWeight == 0 {
		return 0
	}
	return sum / totalWeight
}

// segmentScore rates one folder segment against the best course word it can
// find. The graded fallbacks below the exact/prefix cases are what make
// real-world abbreviations work: "Data" is not a prefix of
// "Datenstrukturen" (it is "Dat" plus a borrowed English ending) and "tech"
// sits in the middle of "Softwaretechnologie", yet both are obviously the
// same word to a human.
func segmentScore(seg string, tokens []string) float64 {
	best := 0.0
	for _, token := range tokens {
		var s float64
		switch {
		case seg == token:
			s = 1.0
		case runeLen(seg) >= 2 && strings.HasPrefix(token, seg):
			s = 0.9
		case runeLen(token) >= 3 && strings.HasPrefix(seg, token):
			s = 0.8
		default:
			if cp := commonPrefixLen(seg, token); cp >= 3 && cp >= runeLen(seg)-1 {
				s = 0.75
			} else if runeLen(seg) >= 4 && strings.Contains(token, seg) {
				s = 0.7
			}
		}
		if s > best {
			best = s
		}
	}
	return best
}

// initials returns the first letter of every non-numeric word.
func initials(words []string) string {
	var b strings.Builder
	for _, w := range words {
		if isNumeric(w) || w == "" {
			continue
		}
		b.WriteRune([]rune(w)[0])
	}
	return b.String()
}

// acronymScore covers the case where the abbreviating happened on the OPAL
// side rather than in the folder tree: the course is called "2026 LA20" and
// the folder spells the subject out as "Lineare Algebra". Word-by-word
// matching sees nothing at all there ("la" shares one letter with
// "lineare"), so the initials have to be compared directly.
//
// Both directions are checked, since either side may be the abbreviated
// one; the reverse direction is scored slightly lower because a short
// all-caps folder name is a weaker signal than a course code that spells
// out an existing folder.
func acronymScore(segments, tokens []string) float64 {
	folderInitials := initials(segments)
	if runeLen(folderInitials) >= 2 {
		for _, token := range tokens {
			if token == folderInitials {
				return 0.9
			}
		}
	}

	if len(segments) == 1 && runeLen(segments[0]) >= 2 && segments[0] == initials(tokens) {
		return 0.85
	}

	return 0
}

// compoundScore covers a folder name cut out of a single German compound
// word: "SoTech" out of "Softwaretechnologie". Every segment must occur in
// one course word, in order, with the first segment at that word's start —
// specific enough that an accidental match is unlikely, which is why it
// scores as high as a clean word-by-word match.
func compoundScore(segments, tokens []string) float64 {
	var meaningful []string
	for _, seg := range segments {
		if !isNumeric(seg) {
			meaningful = append(meaningful, seg)
		}
	}
	if len(meaningful) < 2 {
		return 0
	}

	for _, token := range tokens {
		pos, matched, ok := 0, 0, true
		for i, seg := range meaningful {
			idx := strings.Index(token[pos:], seg)
			if idx < 0 || (i == 0 && idx != 0) {
				ok = false
				break
			}
			pos += idx + len(seg)
			matched += runeLen(seg)
		}
		if ok && matched >= 4 {
			return 0.9
		}
	}

	return 0
}
