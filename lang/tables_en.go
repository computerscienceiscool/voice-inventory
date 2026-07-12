package lang

import "math"

var englishTable = &Table{
	Code: English,

	LocationNouns: set("bin", "location", "shelf", "rack", "aisle", "row", "bay",
		"slot", "area", "zone", "section", "dock", "cage", "pallet"),
	KeepAnchorNouns: set("shelf", "rack", "aisle", "row", "bay", "slot", "area",
		"zone", "section", "dock", "cage", "pallet"),
	LocationPreps: set("in", "at", "on"),
	Articles:      set("the"),

	Ones: map[string]float64{
		"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11,
		"twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15,
		"sixteen": 16, "seventeen": 17, "eighteen": 18, "nineteen": 19,
	},
	Tens: map[string]float64{
		"twenty": 20, "thirty": 30, "forty": 40, "fifty": 50,
		"sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90,
	},
	Hundreds:      map[string]float64{},
	ScaleHundred:  set("hundred"),
	ScaleBig:      map[string]float64{"thousand": 1000, "million": 1e6},
	DozenWords:    set("dozen", "dozens"),
	HalfWords:     set("half"),
	NumConnectors: set("and"),
	NumArticles:   set("a", "an"),
	approxSet: NewPhraseSet(
		Phrase{"about"}, Phrase{"around"}, Phrase{"approximately"},
		Phrase{"roughly"}, Phrase{"maybe"}, Phrase{"like"},
		Phrase{"nearly"}, Phrase{"almost"}, Phrase{"close", "to"},
		Phrase{"more", "or", "less"}, Phrase{"some"},
	),
	vagues: []vagueEntry{
		{Phrase{"a", "couple"}, 2},
		{Phrase{"couple"}, 2},
		{Phrase{"a", "few"}, 3},
		{Phrase{"a", "handful"}, 3},
		{Phrase{"several"}, math.NaN()},
		{Phrase{"some"}, math.NaN()},
		{Phrase{"many"}, math.NaN()},
		{Phrase{"a", "bunch"}, math.NaN()},
		{Phrase{"a", "lot"}, math.NaN()},
		{Phrase{"lots"}, math.NaN()},
	},

	Units: func() map[string]string {
		u := map[string]string{}
		add := func(canon string, forms ...string) {
			u[canon] = canon
			for _, f := range forms {
				u[f] = canon
			}
		}
		add("box", "boxes")
		add("spool", "spools")
		add("reel", "reels")
		add("bag", "bags")
		add("each", "ea")
		add("piece", "pieces", "pcs", "pc")
		add("foot", "feet", "ft")
		add("meter", "meters", "metre", "metres")
		add("inch", "inches")
		add("yard", "yards")
		add("roll", "rolls")
		add("case", "cases")
		add("carton", "cartons")
		add("unit", "units")
		add("pack", "packs", "package", "packages")
		add("can", "cans")
		add("bottle", "bottles")
		add("coil", "coils")
		add("pair", "pairs")
		add("set", "sets")
		add("sheet", "sheets")
		add("tube", "tubes")
		add("drum", "drums")
		add("bucket", "buckets")
		add("crate", "crates")
		add("bundle", "bundles")
		add("length", "lengths")
		add("kilogram", "kilograms", "kilo", "kilos", "kg")
		add("gram", "grams")
		add("pound", "pounds", "lb", "lbs")
		add("liter", "liters", "litre", "litres")
		add("gallon", "gallons")
		return u
	}(),
	Of: set("of"),

	ConfirmWords: set("yes", "yeah", "yep", "yup", "correct", "confirm",
		"confirmed", "right", "ok", "okay", "affirmative", "good", "save"),
	RejectWords: set("no", "nope", "wrong", "incorrect", "negative"),
	scratchSet: NewPhraseSet(
		Phrase{"scratch", "that"}, Phrase{"scratch", "it"},
		Phrase{"delete"}, Phrase{"delete", "that"}, Phrase{"delete", "it"},
		Phrase{"cancel"}, Phrase{"cancel", "that"},
		Phrase{"discard"}, Phrase{"discard", "that"},
		Phrase{"never", "mind"}, Phrase{"nevermind"},
	),
	negationSet: NewPhraseSet(
		Phrase{"no"}, Phrase{"nope"}, Phrase{"wait"}, Phrase{"sorry"},
		Phrase{"actually"}, Phrase{"correction"},
		Phrase{"i", "mean"}, Phrase{"make", "that"}, Phrase{"make", "it"},
	),
	FieldAliases: map[string]string{
		"location": "location", "loc": "location", "place": "location",
		"quantity": "quantity", "qty": "quantity", "count": "quantity",
		"number": "quantity", "amount": "quantity",
		"item": "item", "part": "item", "product": "item", "name": "item",
		"unit": "unit", "units": "unit", "uom": "unit",
		"description": "description", "note": "description",
		"notes": "description", "comment": "description", "details": "description",
	},
	IsWords:      set("is", "equals"),
	ChangeWords:  set("change", "set", "make", "update"),
	ToWords:      set("to"),
	Conjunctions: set("and", "plus"),
}
