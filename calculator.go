package prayer

import (
	"math"
	"time"

	"github.com/RadhiFadlillah/go-prayer/internal/julianday"
	"github.com/shopspring/decimal"
)

// TimeCorrection is correction for each target time
type TimeCorrection map[Target]time.Duration

// AngleCorrection is value in degree, used to correct hour angle
type AngleCorrection map[Target]float64

// Calculator is calculator that used to calculate the prayer times.
type Calculator struct {
	Latitude          float64
	Longitude         float64
	Elevation         float64
	FajrAngle         float64
	IshaAngle         float64
	MaghribDuration   time.Duration
	CalculationMethod CalculationMethod
	AsrConvention     AsrConvention
	PreciseToSeconds  bool
	IgnoreElevation   bool
	TimeCorrection    TimeCorrection
	AngleCorrection   AngleCorrection

	latitude       decimal.Decimal
	longitude      decimal.Decimal
	fajrAngle      decimal.Decimal
	ishaAngle      decimal.Decimal
	asrCoefficient decimal.Decimal

	date           time.Time
	timezone       decimal.Decimal
	transitTime    decimal.Decimal
	sunDeclination decimal.Decimal
}

// Init initiates the calculator.
func (calc *Calculator) Init() *Calculator {
	// Save location
	calc.latitude = decimal.NewFromFloat(calc.Latitude)
	calc.longitude = decimal.NewFromFloat(calc.Longitude)

	// Apply calculation method
	var maghribDuration time.Duration
	var fajrAngle, ishaAngle float64

	switch calc.CalculationMethod {
	case Default, MWL, Algerian, Diyanet:
		fajrAngle, ishaAngle = 18, 17
	case ISNA:
		fajrAngle, ishaAngle = 15, 15
	case UmmAlQura:
		fajrAngle, maghribDuration = 18.5, 90*time.Minute
	case Gulf:
		fajrAngle, maghribDuration = 19.5, 90*time.Minute
	case Karachi, France18, Tunisia:
		fajrAngle, ishaAngle = 18, 18
	case Egypt:
		fajrAngle, ishaAngle = 19.5, 17.5
	case EgyptBis, Kemenag, MUIS, JAKIM:
		fajrAngle, ishaAngle = 20, 18
	case UOIF:
		fajrAngle, ishaAngle = 12, 12
	case France15:
		fajrAngle, ishaAngle = 15, 15
	case Tehran:
		fajrAngle, ishaAngle = 17.7, 14
	case Jafari:
		fajrAngle, ishaAngle = 16, 14
	}

	if calc.FajrAngle != 0 {
		fajrAngle = calc.FajrAngle
	}

	if calc.IshaAngle != 0 {
		ishaAngle = calc.IshaAngle
	}

	if calc.MaghribDuration != 0 {
		maghribDuration = calc.MaghribDuration
	}

	calc.fajrAngle = decimal.NewFromFloat(fajrAngle)
	calc.ishaAngle = decimal.NewFromFloat(ishaAngle)
	calc.MaghribDuration = maghribDuration

	// Set asr coefficient
	switch calc.AsrConvention {
	case Hanafi:
		calc.asrCoefficient = decimal.New(2, 0)
	default:
		calc.asrCoefficient = decimal.New(1, 0)
	}

	return calc
}

// SetDate specifies active date to calculate.
// It will also calculates the timezone from the date location.
func (calc *Calculator) SetDate(date time.Time) *Calculator {
	// Make sure date is at 12 local time
	y := date.Year()
	m := date.Month()
	d := date.Day()
	location := date.Location()
	calc.date = time.Date(y, m, d, 12, 0, 0, 0, location)

	// Save timezone
	_, utcOffset := calc.date.Zone()
	calc.timezone = decimal.New(int64(utcOffset), 0).
		Div(decimal.New(3600, 0))

	// Calculate transit time and sun declination
	jd := julianday.Convert(calc.date)
	calc.transitTime = calc.getTransitTime(jd)
	calc.sunDeclination = calc.getSunDeclination(jd)
	return calc
}

// Calculate calculates time for the specified target.
// Returns the target time and boolean to mark whether the time is available or not.
func (calc Calculator) Calculate(target Target) (time.Time, bool) {
	// If target is Isha and Maghrib duration is specified, just add it
	if target == Isha && calc.MaghribDuration != 0 {
		targetTime, isNA := calc.Calculate(Maghrib)
		if isNA {
			return time.Time{}, true
		}

		return targetTime.Add(calc.MaghribDuration), false
	}

	// Prepare necessary variables
	var targetTime time.Time
	jd := julianday.Convert(calc.date)
	transitTime := calc.transitTime
	sunDeclination := calc.sunDeclination
	sunAltitude := calc.getSunAltitude(target, jd)

	// Max five tries
	for i := 0; i < 5; i++ {
		// Calculate hours to reach the target
		dec15 := decimal.New(15, 0)
		hourAngle, isNA := calc.getHourAngle(sunAltitude, sunDeclination)
		if isNA {
			return time.Time{}, true
		}

		var hours decimal.Decimal
		switch {
		case target > Zuhr:
			hours = transitTime.Add(hourAngle.Div(dec15))
		case target < Zuhr:
			hours = transitTime.Sub(hourAngle.Div(dec15))
		default:
			hours = transitTime
		}

		// Add angle correction
		if correction, exist := calc.AngleCorrection[target]; exist {
			decCorrection := decimal.NewFromFloat(correction)
			hours = hours.Add(decCorrection.Div(dec15))
		}

		// Add time correction
		if correction, exist := calc.TimeCorrection[target]; exist {
			hours = hours.Add(decimal.NewFromFloat(correction.Hours()))
		}

		// Compare time between current and previous iteration
		prevTargetTime := targetTime
		targetTime = calc.hoursToTime(hours)
		diff := prevTargetTime.Sub(targetTime).Seconds()
		if math.Round(diff) == 0 {
			break
		}

		// Improve variables using the result in this iteration
		jd = julianday.Convert(targetTime)
		transitTime = calc.getTransitTime(jd)
		sunDeclination = calc.getSunDeclination(jd)

		if target == Asr {
			sunAltitude = calc.getSunAltitude(target, jd)
		}
	}

	return targetTime, false
}

// CalculateAll returns times for all possible targets. If the target
// is not available, it will be omitted from result.
func (calc Calculator) CalculateAll() map[Target]time.Time {
	result := map[Target]time.Time{}
	for target := Fajr; target <= Isha; target++ {
		if targetTime, isNA := calc.Calculate(target); !isNA {
			result[target] = targetTime
		}
	}

	return result
}
