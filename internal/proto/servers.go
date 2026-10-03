package proto

// Servers maps known server ids to short names (from the community reference
// implementation). Unknown ids are simply shown without a server tag.
var Servers = map[int]string{
	2001: "ISR", 2002: "SIE", 2003: "TRI", 2004: "LUM", 2005: "MAR", 2006: "AZP", 2007: "ERE",
	2008: "BER", 2009: "NEM", 2010: "HAD", 2011: "LUD", 2012: "ULG", 2013: "MUN", 2014: "ODA",
	2015: "ZEN", 2016: "KRO", 2017: "KUI", 2018: "BAB", 2019: "FAF", 2020: "IND", 2021: "ISH",
	1001: "SIE", 1002: "NEZ", 1003: "VAI", 1004: "KAI", 1005: "YUS", 1006: "ARI", 1007: "FRE",
	1008: "MES", 1009: "HIT", 1010: "NAN", 1011: "TAH", 1012: "LUT", 1013: "PER", 1014: "DAM",
	1015: "KAS", 1016: "BAK", 1017: "CHE", 1018: "KOS", 1019: "ISH", 1020: "TIA", 1021: "POE",
}

func KnownServer(id int) bool { _, ok := Servers[id]; return ok }

func ServerName(id int) string { return Servers[id] }
