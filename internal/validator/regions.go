package validator

type RegionGroup struct {
	Group string       `json:"group"`
	Items []RegionItem `json:"items"`
}

type RegionItem struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

var regionGroups = []RegionGroup{
	{
		Group: "North America",
		Items: []RegionItem{
			{"us-ashburn-1", "US East (Ashburn)"},
			{"us-phoenix-1", "US West (Phoenix)"},
			{"us-sanjose-1", "US West (San Jose)"},
			{"us-chicago-1", "US Midwest (Chicago)"},
			{"ca-montreal-1", "Canada Southeast (Montreal)"},
			{"ca-toronto-1", "Canada Southeast (Toronto)"},
			{"mx-queretaro-1", "Mexico Central (Queretaro)"},
			{"mx-monterrey-1", "Mexico Northeast (Monterrey)"},
		},
	},
	{
		Group: "South America",
		Items: []RegionItem{
			{"sa-saopaulo-1", "Brazil East (Sao Paulo)"},
			{"sa-vinhedo-1", "Brazil Southeast (Vinhedo)"},
			{"sa-santiago-1", "Chile Central (Santiago)"},
			{"sa-valparaiso-1", "Chile West (Valparaiso)"},
			{"sa-bogota-1", "Colombia Central (Bogota)"},
		},
	},
	{
		Group: "Europe",
		Items: []RegionItem{
			{"uk-london-1", "UK South (London)"},
			{"uk-cardiff-1", "UK West (Newport)"},
			{"eu-frankfurt-1", "Germany Central (Frankfurt)"},
			{"eu-amsterdam-1", "Netherlands Northwest (Amsterdam)"},
			{"eu-zurich-1", "Switzerland North (Zurich)"},
			{"eu-paris-1", "France Central (Paris)"},
			{"eu-marseille-1", "France South (Marseille)"},
			{"eu-madrid-1", "Spain Central (Madrid)"},
			{"eu-madrid-3", "Spain Central (Madrid 3)"},
			{"eu-milan-1", "Italy Northwest (Milan)"},
			{"eu-turin-1", "Italy North (Turin)"},
			{"eu-stockholm-1", "Sweden Central (Stockholm)"},
			{"eu-jovanovac-1", "Serbia Central (Jovanovac)"},
		},
	},
	{
		Group: "Middle East & Africa",
		Items: []RegionItem{
			{"me-jeddah-1", "Saudi Arabia West (Jeddah)"},
			{"me-riyadh-1", "Saudi Arabia Central (Riyadh)"},
			{"me-dubai-1", "UAE East (Dubai)"},
			{"me-abudhabi-1", "UAE Central (Abu Dhabi)"},
			{"il-jerusalem-1", "Israel Central (Jerusalem)"},
			{"af-johannesburg-1", "South Africa Central (Johannesburg)"},
			{"af-casablanca-1", "Morocco West (Casablanca)"},
		},
	},
	{
		Group: "Asia Pacific",
		Items: []RegionItem{
			{"ap-tokyo-1", "Japan East (Tokyo)"},
			{"ap-osaka-1", "Japan Central (Osaka)"},
			{"ap-seoul-1", "South Korea Central (Seoul)"},
			{"ap-chuncheon-1", "South Korea North (Chuncheon)"},
			{"ap-mumbai-1", "India West (Mumbai)"},
			{"ap-hyderabad-1", "India South (Hyderabad)"},
			{"ap-sydney-1", "Australia East (Sydney)"},
			{"ap-melbourne-1", "Australia Southeast (Melbourne)"},
			{"ap-singapore-1", "Singapore (Singapore)"},
			{"ap-singapore-2", "Singapore West (Singapore)"},
			{"ap-batam-1", "Indonesia North (Batam)"},
			{"ap-kulai-2", "Malaysia West (Kulai)"},
		},
	},
}

func RegionGroups() []RegionGroup {
	return regionGroups
}
