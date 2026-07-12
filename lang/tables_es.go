package lang

import "math"

// All Spanish table keys are stored folded (accent-stripped): "ubicación"
// is looked up as "ubicacion". Fold() applies the same transform to input.
var spanishTable = &Table{
	Code: Spanish,

	LocationNouns: set("bin", "contenedor", "ubicacion", "estante", "estanteria",
		"anaquel", "pasillo", "fila", "rack", "bahia", "ranura", "zona",
		"seccion", "area", "casilla", "cajon", "deposito", "lugar"),
	KeepAnchorNouns: set("estante", "estanteria", "anaquel", "pasillo", "fila",
		"rack", "bahia", "ranura", "zona", "seccion", "area"),
	LocationPreps: set("en"),
	Articles:      set("el", "la", "los", "las", "del", "al"),

	Ones: map[string]float64{
		"cero": 0, "un": 1, "uno": 1, "una": 1, "dos": 2, "tres": 3,
		"cuatro": 4, "cinco": 5, "seis": 6, "siete": 7, "ocho": 8, "nueve": 9,
		"diez": 10, "once": 11, "doce": 12, "trece": 13, "catorce": 14,
		"quince": 15, "dieciseis": 16, "diecisiete": 17, "dieciocho": 18,
		"diecinueve": 19, "veintiuno": 21, "veintiun": 21, "veintiuna": 21,
		"veintidos": 22, "veintitres": 23, "veinticuatro": 24,
		"veinticinco": 25, "veintiseis": 26, "veintisiete": 27,
		"veintiocho": 28, "veintinueve": 29,
	},
	Tens: map[string]float64{
		"veinte": 20, "treinta": 30, "cuarenta": 40, "cincuenta": 50,
		"sesenta": 60, "setenta": 70, "ochenta": 80, "noventa": 90,
	},
	Hundreds: map[string]float64{
		"doscientos": 200, "doscientas": 200, "trescientos": 300,
		"trescientas": 300, "cuatrocientos": 400, "cuatrocientas": 400,
		"quinientos": 500, "quinientas": 500, "seiscientos": 600,
		"seiscientas": 600, "setecientos": 700, "setecientas": 700,
		"ochocientos": 800, "ochocientas": 800, "novecientos": 900,
		"novecientas": 900,
	},
	ScaleHundred:  set("cien", "ciento"),
	ScaleBig:      map[string]float64{"mil": 1000, "millon": 1e6, "millones": 1e6},
	DozenWords:    set("docena", "docenas"),
	HalfWords:     set("media", "medio"),
	NumConnectors: set("y"),
	NumArticles:   set("un", "una", "uno"),
	approxSet: NewPhraseSet(
		Phrase{"unos"}, Phrase{"unas"}, Phrase{"aproximadamente"},
		Phrase{"como"}, Phrase{"casi"}, Phrase{"mas", "o", "menos"},
		Phrase{"cerca", "de"}, Phrase{"alrededor", "de"}, Phrase{"por", "ahi"},
	),
	vagues: []vagueEntry{
		{Phrase{"un", "par"}, 2},
		{Phrase{"unos", "pocos"}, 3},
		{Phrase{"unas", "pocas"}, 3},
		{Phrase{"un", "punado"}, 3},
		{Phrase{"varios"}, math.NaN()},
		{Phrase{"varias"}, math.NaN()},
		{Phrase{"algunos"}, math.NaN()},
		{Phrase{"algunas"}, math.NaN()},
		{Phrase{"muchos"}, math.NaN()},
		{Phrase{"muchas"}, math.NaN()},
		{Phrase{"un", "monton"}, math.NaN()},
	},

	Units: func() map[string]string {
		u := map[string]string{}
		add := func(canon string, forms ...string) {
			u[canon] = canon
			for _, f := range forms {
				u[f] = canon
			}
		}
		add("caja", "cajas")
		add("carrete", "carretes")
		add("bobina", "bobinas")
		add("bolsa", "bolsas")
		add("pieza", "piezas")
		add("unidad", "unidades")
		add("rollo", "rollos")
		add("paquete", "paquetes")
		add("lata", "latas")
		add("botella", "botellas")
		add("juego", "juegos")
		add("lamina", "laminas")
		add("tubo", "tubos")
		add("tambor", "tambores")
		add("cubeta", "cubetas")
		add("caja", "cajitas")
		add("metro", "metros")
		add("pie", "pies")
		add("pulgada", "pulgadas")
		add("kilo", "kilos", "kilogramo", "kilogramos")
		add("gramo", "gramos")
		add("litro", "litros")
		add("galon", "galones")
		add("par", "pares")
		return u
	}(),
	Of: set("de", "del"),

	ConfirmWords: set("si", "correcto", "confirmar", "confirmado", "dale",
		"vale", "claro", "afirmativo", "ok", "okay", "listo", "bueno", "guarda"),
	RejectWords: set("no", "incorrecto", "mal", "negativo"),
	scratchSet: NewPhraseSet(
		Phrase{"borra", "eso"}, Phrase{"borralo"}, Phrase{"borrar"},
		Phrase{"cancela"}, Phrase{"cancelar"}, Phrase{"cancela", "eso"},
		Phrase{"elimina", "eso"}, Phrase{"eliminalo"}, Phrase{"eliminar"},
		Phrase{"descarta"}, Phrase{"descartalo"}, Phrase{"olvidalo"},
		Phrase{"tacha", "eso"}, Phrase{"anula"},
	),
	negationSet: NewPhraseSet(
		Phrase{"no"}, Phrase{"digo"}, Phrase{"perdon"}, Phrase{"espera"},
		Phrase{"mejor", "dicho"}, Phrase{"corrijo"}, Phrase{"en", "realidad"},
	),
	FieldAliases: map[string]string{
		"ubicacion": "location", "lugar": "location", "sitio": "location",
		"bin": "location",
		"cantidad": "quantity", "numero": "quantity", "cuenta": "quantity",
		"articulo": "item", "pieza": "item", "parte": "item",
		"producto": "item", "item": "item", "nombre": "item",
		"unidad": "unit", "unidades": "unit",
		"descripcion": "description", "nota": "description",
		"notas": "description", "comentario": "description",
		"detalle": "description", "detalles": "description",
	},
	IsWords:      set("es"),
	ChangeWords:  set("cambia", "cambiar", "pon", "poner", "actualiza"),
	ToWords:      set("a", "por"),
	Conjunctions: set("y", "e"),
}
