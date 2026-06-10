package expand

import "strings"

// Result holds the expanded query variants.
type Result struct {
	Original       string   // the input query
	Reformulations []string // alternative phrasings
	Keywords       []string // cross-lingual translation terms
}

// Expand produces query reformulations and cross-lingual keywords.
//
// Reformulations strip question framing so embeddings focus on the
// core concept. Cross-lingual keywords bridge Chinese↔English tech
// terms so the lexical lanes (BM25, trigram, field) can match content
// written in the other language. Keywords do NOT affect the vector
// lane (embedding is computed before expansion) or the temporal lane
// (time-based, ignores query text). The keyword list is capped at
// maxCrossLingualTerms to avoid diluting search signal.
func Expand(query string) *Result {
	result := &Result{Original: query}
	lower := strings.ToLower(strings.TrimSpace(query))

	// Strip question prefixes so the embedding targets the concept.
	for _, prefix := range questionPrefixes {
		if strings.HasPrefix(lower, prefix) {
			stripped := strings.TrimSpace(lower[len(prefix):])
			stripped = strings.TrimRight(stripped, "?")
			if len(stripped) > 3 {
				result.Reformulations = append(result.Reformulations, stripped)
			}
			break
		}
	}

	// Cross-lingual term bridging: append translations for CJK↔Latin
	// terms found in the query. This is deterministic, zero-latency,
	// and runs unconditionally (unlike the LLM distiller).
	result.Keywords = crossLingualKeywords(lower)

	return result
}

const maxCrossLingualTerms = 5

// crossLingualKeywords scans the query for known CJK↔Latin tech terms
// and returns translations. Bidirectional: Chinese queries get English
// translations, English queries get Chinese translations.
//
// Uses ordered slices (not map iteration) so output is deterministic
// across runs — important because the result affects searchQuery,
// field-lane patterns, and eval reproducibility.
func crossLingualKeywords(query string) []string {
	// Collect terms already present in the query to avoid duplicates.
	present := make(map[string]bool)
	for _, term := range strings.Fields(query) {
		present[term] = true
	}

	var keywords []string
	seen := make(map[string]bool)

	// Check CJK→English: scan for CJK substrings in the query.
	// Iterate in definition order (zhToEnPairs) for determinism.
	for _, pair := range zhToEnPairs {
		if strings.Contains(query, pair.zh) && !present[pair.en] && !seen[pair.en] {
			seen[pair.en] = true
			keywords = append(keywords, pair.en)
			if len(keywords) >= maxCrossLingualTerms {
				return keywords
			}
		}
	}

	// Check English→CJK: scan for English words in the query.
	// Use word boundaries to avoid false positives from partial matches
	// (e.g., "testing" should not match "test").
	queryWords := latinWords(query)
	for _, word := range queryWords {
		if zhTerm, ok := enToZh[word]; ok && !seen[zhTerm] {
			// Verify the CJK term isn't already in the query
			if !strings.Contains(query, zhTerm) {
				seen[zhTerm] = true
				keywords = append(keywords, zhTerm)
				if len(keywords) >= maxCrossLingualTerms {
					return keywords
				}
			}
		}
	}

	return keywords
}

// latinWords extracts lowercased Latin-script words from a mixed query.
func latinWords(query string) []string {
	var words []string
	var buf strings.Builder
	for _, r := range query {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			buf.WriteRune(r)
		} else {
			if buf.Len() >= 2 {
				words = append(words, buf.String())
			}
			buf.Reset()
		}
	}
	if buf.Len() >= 2 {
		words = append(words, buf.String())
	}
	return words
}

// --- Cross-lingual tech term dictionary ---
//
// Bidirectional Chinese↔English mappings for common tech terms.
// Stored as an ordered slice so iteration is deterministic (Go map
// iteration is randomized, which would make searchQuery and eval
// results non-reproducible when the cap is hit).
//
// This list is intentionally conservative: only terms with stable,
// unambiguous translations. It grows organically as new terms
// surface in eval failures or user feedback.
//
// Maintenance: add terms to zhToEnPairs, then run the expand tests
// to verify no false-positive matches from substring overlap.

type zhEnPair struct {
	zh string
	en string
}

var zhToEnPairs = []zhEnPair{
	// ── Infrastructure & ops ──
	{"邮件", "email"},
	{"数据库", "database"},
	{"缓存", "cache"},
	{"部署", "deploy"},
	{"服务器", "server"},
	{"队列", "queue"},
	{"监控", "monitoring"},
	{"日志", "logging"},
	{"迁移", "migration"},
	{"索引", "index"},
	{"搜索", "search"},
	{"向量", "vector"},
	{"集群", "cluster"},
	{"负载均衡", "load-balancing"},
	{"容器", "container"},
	{"镜像", "image"},
	{"实例", "instance"},
	{"域名", "domain"},
	{"证书", "certificate"},
	{"存储", "storage"},
	{"备份", "backup"},
	{"恢复", "recovery"},
	{"扩容", "scaling"},
	{"限流", "rate-limiting"},
	{"熔断", "circuit-breaker"},
	{"消息", "message"},
	{"通知", "notification"},
	{"推送", "push"},
	{"定时任务", "cron"},
	{"健康检查", "health-check"},

	// ── Architecture & design ──
	{"架构", "architecture"},
	{"接口", "api"},
	{"配置", "config"},
	{"认证", "auth"},
	{"授权", "authorization"},
	{"权限", "permissions"},
	{"路由", "routing"},
	{"中间件", "middleware"},
	{"网关", "gateway"},
	{"微服务", "microservice"},
	{"单体", "monolith"},
	{"模块", "module"},
	{"组件", "component"},
	{"插件", "plugin"},
	{"钩子", "hook"},
	{"回调", "callback"},
	{"事件", "event"},
	{"订阅", "subscription"},
	{"发布", "publish"},
	{"协议", "protocol"},
	{"令牌", "token"},
	{"密钥", "secret"},
	{"加密", "encryption"},
	{"哈希", "hash"},
	{"签名", "signature"},
	{"会话", "session"},
	{"状态", "state"},
	{"无状态", "stateless"},
	{"持久化", "persistence"},
	{"序列化", "serialization"},
	{"反序列化", "deserialization"},

	// ── Development ──
	{"测试", "test"},
	{"单元测试", "unit-test"},
	{"集成测试", "integration-test"},
	{"端到端", "end-to-end"},
	{"文档", "documentation"},
	{"选型", "selection"},
	{"重构", "refactor"},
	{"依赖", "dependency"},
	{"版本", "version"},
	{"分支", "branch"},
	{"合并", "merge"},
	{"代码审查", "code-review"},
	{"提交", "commit"},
	{"发布", "release"},
	{"回滚", "rollback"},
	{"调试", "debug"},
	{"断点", "breakpoint"},
	{"堆栈", "stack-trace"},
	{"异常", "exception"},
	{"错误处理", "error-handling"},
	{"类型", "type"},
	{"泛型", "generics"},
	{"并发", "concurrency"},
	{"异步", "async"},
	{"同步", "sync"},
	{"线程", "thread"},
	{"协程", "goroutine"},
	{"锁", "lock"},
	{"死锁", "deadlock"},
	{"竞态", "race-condition"},
	{"编译", "compile"},
	{"构建", "build"},
	{"打包", "bundle"},
	{"链接", "link"},
	{"环境变量", "env"},
	{"脚本", "script"},
	{"命令行", "cli"},
	{"终端", "terminal"},
	{"工具链", "toolchain"},
	{"包管理", "package-manager"},
	{"脚手架", "scaffold"},

	// ── Data & analytics ──
	{"查询", "query"},
	{"事务", "transaction"},
	{"表", "table"},
	{"字段", "field"},
	{"主键", "primary-key"},
	{"外键", "foreign-key"},
	{"关联", "relation"},
	{"聚合", "aggregation"},
	{"分页", "pagination"},
	{"游标", "cursor"},
	{"排序", "sorting"},
	{"过滤", "filter"},
	{"去重", "dedup"},
	{"批量", "batch"},
	{"流式", "streaming"},
	{"管道", "pipeline"},
	{"数据模型", "data-model"},
	{"模式", "schema"},
	{"嵌入", "embedding"},
	{"相似度", "similarity"},
	{"召回", "recall"},
	{"精确度", "precision"},
	{"排名", "ranking"},

	// ── Frontend & UI ──
	{"前端", "frontend"},
	{"后端", "backend"},
	{"页面", "page"},
	{"布局", "layout"},
	{"样式", "style"},
	{"主题", "theme"},
	{"暗黑模式", "dark-mode"},
	{"响应式", "responsive"},
	{"动画", "animation"},
	{"交互", "interaction"},
	{"表单", "form"},
	{"输入", "input"},
	{"按钮", "button"},
	{"弹窗", "modal"},
	{"导航", "navigation"},
	{"侧边栏", "sidebar"},
	{"面包屑", "breadcrumb"},
	{"图标", "icon"},
	{"渲染", "render"},
	{"懒加载", "lazy-loading"},
	{"虚拟列表", "virtual-list"},
	{"国际化", "i18n"},
	{"本地化", "l10n"},
	{"无障碍", "accessibility"},

	// ── DevOps & CI/CD ──
	{"流水线", "pipeline"},
	{"持续集成", "ci"},
	{"持续部署", "cd"},
	{"自动化", "automation"},
	{"测试覆盖率", "coverage"},
	{"代码检查", "lint"},
	{"格式化", "format"},
	{"构建产物", "artifact"},
	{"环境", "environment"},
	{"灰度", "canary"},
	{"蓝绿部署", "blue-green"},
	{"回归", "regression"},

	// ── Cloud & networking ──
	{"云服务", "cloud"},
	{"对象存储", "object-storage"},
	{"文件上传", "upload"},
	{"下载", "download"},
	{"带宽", "bandwidth"},
	{"延迟", "latency"},
	{"超时", "timeout"},
	{"重试", "retry"},
	{"代理", "proxy"},
	{"反向代理", "reverse-proxy"},
	{"跨域", "cors"},
	{"防火墙", "firewall"},
	{"白名单", "whitelist"},

	// ── AI & ML ──
	{"模型", "model"},
	{"训练", "training"},
	{"推理", "inference"},
	{"提示词", "prompt"},
	{"微调", "fine-tuning"},
	{"蒸馏", "distillation"},
	{"分类", "classification"},
	{"聚类", "clustering"},
	{"标注", "annotation"},
	{"语料库", "corpus"},
	{"分词", "tokenization"},

	// ── Product & business ──
	{"定价", "pricing"},
	{"方案", "solution"},
	{"功能", "feature"},
	{"用户", "user"},
	{"反馈", "feedback"},
	{"设计", "design"},
	{"性能", "performance"},
	{"优化", "optimization"},
	{"需求", "requirement"},
	{"迭代", "iteration"},
	{"里程碑", "milestone"},
	{"路线图", "roadmap"},
	{"竞品", "competitor"},
	{"留存", "retention"},
	{"转化", "conversion"},
	{"增长", "growth"},
	{"指标", "metric"},
	{"漏斗", "funnel"},
	{"用户体验", "ux"},
	{"原型", "prototype"},
	{"上线", "launch"},
	{"灰度发布", "staged-rollout"},
	{"工单", "ticket"},
	{"故障", "incident"},
	{"复盘", "postmortem"},

	// ── Security ──
	{"安全", "security"},
	{"漏洞", "vulnerability"},
	{"注入", "injection"},
	{"攻击", "attack"},
	{"审计", "audit"},
	{"合规", "compliance"},
	{"隐私", "privacy"},
	{"脱敏", "redaction"},

	// ── Business & operations ──
	{"合同", "contract"},
	{"预算", "budget"},
	{"发票", "invoice"},
	{"报价", "quote"},
	{"供应商", "vendor"},
	{"客户", "customer"},
	{"收入", "revenue"},
	{"利润", "profit"},
	{"成本", "cost"},
	{"报告", "report"},
	{"会议", "meeting"},
	{"议程", "agenda"},
	{"决议", "resolution"},
	{"招聘", "hiring"},
	{"候选人", "candidate"},
	{"面试", "interview"},
	{"入职", "onboarding"},
	{"离职", "offboarding"},
	{"培训", "training"},
	{"流程", "process"},
	{"审批", "approval"},
	{"政策", "policy"},
	{"项目", "project"},
	{"截止日期", "deadline"},
	{"交付物", "deliverable"},
	{"利益相关者", "stakeholder"},
	{"风险", "risk"},
	{"目标", "goal"},
	{"关键结果", "key-result"},
	{"季度", "quarter"},
	{"年度", "annual"},
}

// enToZh is the reverse mapping for English→CJK lookups.
// Built from zhToEnPairs at init time.
var enToZh map[string]string

func init() {
	enToZh = make(map[string]string, len(zhToEnPairs))
	for _, pair := range zhToEnPairs {
		// First Chinese term wins if multiple map to the same English word.
		if _, exists := enToZh[pair.en]; !exists {
			enToZh[pair.en] = pair.zh
		}
	}
}

var questionPrefixes = []string{
	"what is the ",
	"what are the ",
	"what is our ",
	"what are our ",
	"what is ",
	"what are ",
	"how do we ",
	"how does the ",
	"how does ",
	"how do i ",
	"how to ",
	"where is the ",
	"where is ",
	"where can i find ",
	"why do we ",
	"why does ",
	"why is ",
	"tell me about ",
	"explain ",
	"describe ",
}
