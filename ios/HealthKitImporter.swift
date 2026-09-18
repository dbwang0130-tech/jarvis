import Foundation
import HealthKit

/// 从系统「健康」App 读数据，交给 Go 转成 FHIR。
///
/// 这边**只负责读和翻译成中文名**：把 `HKQuantityTypeIdentifier.bodyMass`
/// 读成「体重 / 72.5 / 某时刻」，再往下就归 Go 管了。LOINC 码、单位、
/// 合理范围、去重那一套只该有一份，两边各写一份迟早对不上。
///
/// **读授权是查不到结果的**——iOS 有意不告诉 app「用户拒了读权限」，
/// 否则光凭「读不到数据」就能反推出用户有没有某种病。所以拿到 0 条时
/// 分不清是拒了还是本来就没有，界面上那句话得把两种可能都说到。
enum HealthKitImporter {
    static var isAvailable: Bool { HKHealthStore.isHealthDataAvailable() }

    private static let store = HKHealthStore()

    /// 一条读数。字段跟 Go 的 health.Sample 一一对应。
    struct Sample: Codable {
        var kind: String
        var value: Double
        var value2: Double = 0
        var at: Date
        var uid: String
        var source: String
    }

    /// 要读的量。键是我们的中文名，值是 HealthKit 的标识和换算。
    ///
    /// 心率取的是**静息心率**而不是每一次心跳读数：后者一天几百条，
    /// 导进来把文件撑爆，看趋势反而更难。静息心率一天一条，正是要看的那个。
    private static let quantities: [(kind: String,
                                    id: HKQuantityTypeIdentifier,
                                    unit: HKUnit)] = [
        ("体重", .bodyMass, .gramUnit(with: .kilo)),
        ("身高", .height, .meterUnit(with: .centi)),
        ("静息心率", .restingHeartRate, HKUnit.count().unitDivided(by: .minute())),
        ("呼吸", .respiratoryRate, HKUnit.count().unitDivided(by: .minute())),
        ("体温", .bodyTemperature, .degreeCelsius()),
        ("血氧", .oxygenSaturation, .percent()),
        ("腰围", .waistCircumference, .meterUnit(with: .centi)),
        ("血糖", .bloodGlucose,
         HKUnit.moleUnit(with: .milli, molarMass: HKUnitMolarMassBloodGlucose)
            .unitDivided(by: .liter())),
    ]

    private static var readTypes: Set<HKObjectType> {
        var out = Set<HKObjectType>()
        for q in quantities {
            if let t = HKQuantityType.quantityType(forIdentifier: q.id) { out.insert(t) }
        }
        for id in [HKQuantityTypeIdentifier.bloodPressureSystolic,
                   .bloodPressureDiastolic, .stepCount] {
            if let t = HKQuantityType.quantityType(forIdentifier: id) { out.insert(t) }
        }
        if let t = HKCategoryType.categoryType(forIdentifier: .sleepAnalysis) { out.insert(t) }
        return out
    }

    /// 要一次授权。用户在系统弹窗里逐项勾选。
    static func requestPermission() async -> Bool {
        guard isAvailable else { return false }
        do {
            try await store.requestAuthorization(toShare: [], read: readTypes)
            return true
        } catch {
            return false
        }
    }

    /// 把最近 days 天的数据读出来。
    ///
    /// 重复读同一段时间没关系：每条都带着 HealthKit 的 UUID，Go 那边靠它
    /// 认出已经导过的。所以不需要记「上次导到哪儿」——那种游标一旦跟真实
    /// 情况错位（换机、恢复备份、补录旧数据），就会漏掉一整段而没人发现。
    static func read(days: Int = 90) async -> [Sample] {
        guard isAvailable else { return [] }
        let now = Date()
        guard let from = Calendar.current.date(byAdding: .day, value: -days, to: now) else {
            return []
        }

        var out: [Sample] = []
        for q in quantities {
            out += await quantitySamples(kind: q.kind, id: q.id, unit: q.unit, from: from, to: now)
        }
        out += await bloodPressure(from: from, to: now)
        out += await dailySteps(from: from, to: now)
        out += await nightlySleep(from: from, to: now)
        return out
    }

    // MARK: - 逐条的量

    private static func quantitySamples(kind: String, id: HKQuantityTypeIdentifier,
                                        unit: HKUnit, from: Date, to: Date) async -> [Sample] {
        guard let type = HKQuantityType.quantityType(forIdentifier: id) else { return [] }
        let samples = await query(type: type, from: from, to: to)
        return samples.compactMap { s in
            guard let q = s as? HKQuantitySample, q.quantity.is(compatibleWith: unit) else {
                return nil
            }
            var value = q.quantity.doubleValue(for: unit)
            // 血氧 HealthKit 给的是 0~1 的比例，我们记的是百分数。
            if id == .oxygenSaturation { value *= 100 }
            return Sample(kind: kind, value: value, at: q.endDate,
                          uid: q.uuid.uuidString, source: q.sourceRevision.source.name)
        }
    }

    // MARK: - 血压

    /// 血压在 HealthKit 里是一条 correlation，里面装着收缩压和舒张压两条。
    /// 拆开各导各的话，两个数就不再是「一次量的」了——而血压只有成对才有意义。
    private static func bloodPressure(from: Date, to: Date) async -> [Sample] {
        guard let type = HKCorrelationType.correlationType(forIdentifier: .bloodPressure),
              let sysType = HKQuantityType.quantityType(forIdentifier: .bloodPressureSystolic),
              let diaType = HKQuantityType.quantityType(forIdentifier: .bloodPressureDiastolic)
        else { return [] }

        let unit = HKUnit.millimeterOfMercury()
        return await query(type: type, from: from, to: to).compactMap { s in
            guard let c = s as? HKCorrelation,
                  let sys = c.objects(for: sysType).first as? HKQuantitySample,
                  let dia = c.objects(for: diaType).first as? HKQuantitySample
            else { return nil }
            return Sample(kind: "血压",
                          value: sys.quantity.doubleValue(for: unit),
                          value2: dia.quantity.doubleValue(for: unit),
                          at: c.endDate, uid: c.uuid.uuidString,
                          source: c.sourceRevision.source.name)
        }
    }

    // MARK: - 步数（按天合计）

    /// 步数要的是「这一天走了多少」，不是几百条零散样本。
    ///
    /// **今天不导**：还没走完，导进去就是个半截数，而它带着当天的
    /// 合成 id，明天再导会被当成「已经有了」跳过，那个半截数就永远留在
    /// 那儿了。等这一天过完再说。
    private static func dailySteps(from: Date, to: Date) async -> [Sample] {
        guard let type = HKQuantityType.quantityType(forIdentifier: .stepCount) else { return [] }
        let cal = Calendar.current
        let anchor = cal.startOfDay(for: from)
        let today = cal.startOfDay(for: to)

        let stats = await statistics(type: type, from: anchor, to: to,
                                     interval: DateComponents(day: 1))
        var out: [Sample] = []
        for s in stats {
            let day = cal.startOfDay(for: s.startDate)
            guard day < today, let sum = s.sumQuantity() else { continue }
            let steps = sum.doubleValue(for: .count())
            guard steps > 0 else { continue }
            let key = dayKey(day)
            // 合计出来的数没有自己的 UUID，用日期造一个稳定的：
            // 同一天再导还是这个 id，认得出来。
            out.append(Sample(kind: "步数", value: steps.rounded(),
                              at: cal.date(byAdding: .second, value: 86399, to: day) ?? day,
                              uid: "steps:\(key)", source: "健康"))
        }
        return out
    }

    // MARK: - 睡眠（按夜合计）

    /// 睡眠要的是「这一觉睡了几小时」。
    ///
    /// HealthKit 存的是一段段区间，还分核心/深度/REM/清醒。只把真正睡着的
    /// 那几类加起来——把「在床上」也算进去的话，躺着刷手机的一小时也成了睡眠。
    ///
    /// 归夜：按段落**结束时间减 12 小时**落在哪一天算。凌晨 3 点睡到 7 点，
    /// 减 12 小时还在前一天，归到「昨晚」；中午的午觉减完落在当天，
    /// 归到当天那一夜，跟晚上的合在一起——这是有意的，一天的总睡眠时长
    /// 比分开记更有用。
    private static func nightlySleep(from: Date, to: Date) async -> [Sample] {
        guard let type = HKCategoryType.categoryType(forIdentifier: .sleepAnalysis) else {
            return []
        }
        let asleep: Set<Int> = [
            HKCategoryValueSleepAnalysis.asleepCore.rawValue,
            HKCategoryValueSleepAnalysis.asleepDeep.rawValue,
            HKCategoryValueSleepAnalysis.asleepREM.rawValue,
            HKCategoryValueSleepAnalysis.asleepUnspecified.rawValue,
        ]
        let cal = Calendar.current
        let todayKey = dayKey(cal.startOfDay(for: to.addingTimeInterval(-12 * 3600)))

        var seconds: [String: Double] = [:]
        var wake: [String: Date] = [:]
        var who: [String: String] = [:]
        for s in await query(type: type, from: from, to: to) {
            guard let c = s as? HKCategorySample, asleep.contains(c.value) else { continue }
            let night = cal.startOfDay(for: c.endDate.addingTimeInterval(-12 * 3600))
            let key = dayKey(night)
            seconds[key, default: 0] += c.endDate.timeIntervalSince(c.startDate)
            if wake[key] == nil || c.endDate > wake[key]! { wake[key] = c.endDate }
            who[key] = c.sourceRevision.source.name
        }

        var out: [Sample] = []
        for (key, secs) in seconds {
            // 今晚还没睡完，跟步数同理：先别导，导了就定死在半截上。
            guard key != todayKey, secs > 0 else { continue }
            out.append(Sample(kind: "睡眠", value: (secs / 3600 * 10).rounded() / 10,
                              at: wake[key] ?? Date(),
                              uid: "sleep:\(key)", source: who[key] ?? "健康"))
        }
        return out
    }

    // MARK: - 查询

    private static func dayKey(_ d: Date) -> String {
        let c = Calendar.current.dateComponents([.year, .month, .day], from: d)
        return String(format: "%04d-%02d-%02d", c.year ?? 0, c.month ?? 0, c.day ?? 0)
    }

    private static func query(type: HKSampleType, from: Date, to: Date) async -> [HKSample] {
        let predicate = HKQuery.predicateForSamples(withStart: from, end: to,
                                                    options: .strictStartDate)
        return await withCheckedContinuation { cont in
            let q = HKSampleQuery(sampleType: type, predicate: predicate,
                                  limit: HKObjectQueryNoLimit, sortDescriptors: nil) { _, res, _ in
                // 读权限被拒也是这条路（res 为 nil），跟「本来就没数据」
                // 长得一模一样——iOS 有意不区分，见类型注释。
                cont.resume(returning: res ?? [])
            }
            store.execute(q)
        }
    }

    private static func statistics(type: HKQuantityType, from: Date, to: Date,
                                   interval: DateComponents) async -> [HKStatistics] {
        let predicate = HKQuery.predicateForSamples(withStart: from, end: to,
                                                    options: .strictStartDate)
        return await withCheckedContinuation { cont in
            let q = HKStatisticsCollectionQuery(quantityType: type,
                                                quantitySamplePredicate: predicate,
                                                options: .cumulativeSum,
                                                anchorDate: from,
                                                intervalComponents: interval)
            q.initialResultsHandler = { _, res, _ in
                guard let res else { return cont.resume(returning: []) }
                var out: [HKStatistics] = []
                res.enumerateStatistics(from: from, to: to) { s, _ in out.append(s) }
                cont.resume(returning: out)
            }
            store.execute(q)
        }
    }
}
