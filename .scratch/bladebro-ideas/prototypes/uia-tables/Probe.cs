// Read-only UIA table/grid probe, COM IUIAutomation (same API as winuse).
// Usage: probe.exe <hwnd-decimal-or-0xhex> [maxGrids=4] [cellBudgetSec=60]
// Never calls SetFocus, Select, Invoke, Realize, ScrollIntoView or SetCurrentView.
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Linq;
using System.Text;
using Interop.UIAutomationClient;

static class P
{
    const int GridP = 10006, GridItemP = 10007, TableP = 10012, TableItemP = 10013, ItemContainerP = 10019,
        VirtItemP = 10020, MultiViewP = 10008;
    const int pName = 30005, pCT = 30003, pClass = 30012, pAid = 30011, pFw = 30024, pOffscreen = 30022,
        pGridAvail = 30030, pTableAvail = 30038, pGridItemAvail = 30029, pTableItemAvail = 30039,
        pICAvail = 30108, pVirtAvail = 30109, pGIRow = 30064, pGICol = 30065, pValue = 30045,
        pValueAvail = 30043, pMVAvail = 30032, pScrollAvail = 30034, pSelAvail = 30037;
    static readonly Dictionary<int, string> CT = new Dictionary<int, string> {
        {50000,"Button"},{50004,"Edit"},{50005,"Hyperlink"},{50006,"Image"},{50007,"ListItem"},{50008,"List"},
        {50020,"Text"},{50023,"Tree"},{50024,"TreeItem"},{50025,"Custom"},{50026,"Group"},{50028,"DataGrid"},
        {50029,"DataItem"},{50032,"Window"},{50033,"Pane"},{50034,"Header"},{50035,"HeaderItem"},{50036,"Table"},
        {50002,"CheckBox"},{50003,"ComboBox"},{50021,"ToolBar"},{50014,"ScrollBar"},{50018,"Tab"},{50019,"TabItem"},
        {50031,"SplitButton"},{50011,"MenuItem"},{50030,"Document"},{50037,"TitleBar"},{50038,"Separator"} };
    static string Ct(int id) { string s; return CT.TryGetValue(id, out s) ? s : id.ToString(); }

    static IUIAutomation2 A;
    static bool RealizeMode, SafeMode, ReadOnlyMode;
    static bool IsRowCt(int t) { return t == 50029 || t == 50007 || t == 50025 || t == 50024; }

    static string S(object o) { return o == null ? "" : o.ToString(); }
    static string Cell(IUIAutomationElement e)
    {
        // Value first (DataGridView cell Names are "Col Row N, Not sorted."), then Name.
        if ((bool)e.GetCurrentPropertyValue(pValueAvail)) { string v = S(e.GetCurrentPropertyValue(pValue)); if (v.Length > 0) return v; }
        return e.CurrentName ?? "";
    }
    static string Trunc(string s, int n = 40) { s = (s ?? "").Replace("\r", " ").Replace("\n", " "); return s.Length > n ? s.Substring(0, n) + "…" : s; }
    static bool Has(IUIAutomationElement e, int prop) { try { return (bool)e.GetCurrentPropertyValue(prop); } catch { return false; } }
    static string Desc(IUIAutomationElement e)
    {
        return string.Format("{0} name='{1}' class='{2}' aid='{3}' fw='{4}'", Ct(e.CurrentControlType), Trunc(e.CurrentName, 50),
            e.CurrentClassName, e.CurrentAutomationId, e.CurrentFrameworkId);
    }
    static List<IUIAutomationElement> Arr(IUIAutomationElementArray a)
    {
        var l = new List<IUIAutomationElement>(); if (a == null) return l;
        for (int i = 0; i < a.Length; i++) l.Add(a.GetElement(i)); return l;
    }

    static int Main(string[] args)
    {
        Console.OutputEncoding = Encoding.UTF8;
        string h = args[0];
        long hwnd = h.StartsWith("0x") ? Convert.ToInt64(h.Substring(2), 16) : long.Parse(h);
        int maxGrids = args.Length > 1 ? int.Parse(args[1]) : 4;
        double budget = args.Length > 2 ? double.Parse(args[2]) : 60;
        RealizeMode = args.Length > 3 && args[3] == "realize"; // only for windows this experiment opened
        ReadOnlyMode = args.Length > 3 && args[3] == "readonly"; // windows we did not open: no GetItem (WPF GetItem scrolls), no Realize
        SafeMode = args.Length > 3 && args[3] == "safe"; // huge lists: no FindAll/subtree cache, GetItem first 200 rows
        A = new CUIAutomation8();
        A.ConnectionTimeout = 10000; A.TransactionTimeout = 20000; A.AutoSetFocus = 0;
        var root = A.ElementFromHandle(new IntPtr(hwnd));
        Console.WriteLine("ROOT " + Desc(root));

        var sw = Stopwatch.StartNew();
        var cond = A.CreateOrConditionFromArray(new[] {
            A.CreatePropertyCondition(pGridAvail, true), A.CreatePropertyCondition(pTableAvail, true),
            A.CreatePropertyCondition(pCT, 50008), A.CreatePropertyCondition(pCT, 50028), A.CreatePropertyCondition(pCT, 50036) });
        var cands = Has(root, pGridAvail) ? new List<IUIAutomationElement> { root } : Arr(root.FindAll(TreeScope.TreeScope_Subtree, cond));
        Console.WriteLine("FIND {0} candidates in {1} ms", cands.Count, sw.ElapsedMilliseconds);
        // Prefer ones with Grid pattern, then most children.
        var ordered = cands.Select(e => new { e, g = Has(e, pGridAvail) }).OrderByDescending(x => x.g).Take(maxGrids).ToList();
        if (cands.Count == 0) Dump(root, 0, 7);
        foreach (var c in cands) Console.WriteLine("  cand: " + Desc(c) + " grid=" + Has(c, pGridAvail) + " table=" + Has(c, pTableAvail));
        foreach (var x in ordered) { try { ProbeGrid(x.e, budget); } catch (Exception ex) { Console.WriteLine("  ERROR " + ex.GetType().Name + ": " + ex.Message); } }
        return 0;
    }

    static void Dump(IUIAutomationElement e, int d, int max)
    {
        Console.WriteLine(new string(' ', d * 2) + "- " + Desc(e));
        if (d >= max) return;
        var kids = Arr(e.FindAll(TreeScope.TreeScope_Children, A.CreateTrueCondition()));
        foreach (var k in kids.Take(15)) Dump(k, d + 1, max);
        if (kids.Count > 15) Console.WriteLine(new string(' ', d * 2 + 2) + "... " + kids.Count + " children");
    }

    static void ProbeGrid(IUIAutomationElement g, double budget)
    {
        Console.WriteLine();
        Console.WriteLine("=== GRID " + Desc(g));
        bool hasGrid = Has(g, pGridAvail), hasTable = Has(g, pTableAvail), hasIC = Has(g, pICAvail);
        Console.WriteLine("  patterns: Grid={0} Table={1} ItemContainer={2} MultipleView={3} Scroll={4} Selection={5}",
            hasGrid, hasTable, hasIC, Has(g, pMVAvail), Has(g, pScrollAvail), Has(g, pSelAvail));
        if (Has(g, pMVAvail))
        {
            var mv = (IUIAutomationMultipleViewPattern)g.GetCurrentPattern(MultiViewP);
            int cur = mv.CurrentCurrentView; string vn = ""; try { vn = mv.GetViewName(cur); } catch { }
            Console.WriteLine("  multipleView current={0} '{1}'", cur, vn);
        }
        int rows = -1, cols = -1;
        IUIAutomationGridPattern gp = null;
        if (hasGrid)
        {
            gp = (IUIAutomationGridPattern)g.GetCurrentPattern(GridP);
            rows = gp.CurrentRowCount; cols = gp.CurrentColumnCount;
            Console.WriteLine("  RowCount={0} ColumnCount={1}", rows, cols);
        }
        if (hasTable)
        {
            var tp = (IUIAutomationTablePattern)g.GetCurrentPattern(TableP);
            var sw0 = Stopwatch.StartNew();
            var hdrs = Arr(tp.GetCurrentColumnHeaders());
            Console.WriteLine("  ColumnHeaders ({0}, {1} ms): [{2}]", hdrs.Count, sw0.ElapsedMilliseconds,
                string.Join(" | ", hdrs.Select(e => Trunc(e.CurrentName, 25) + "<" + Ct(e.CurrentControlType) + ">")));
            Console.WriteLine("  RowHeaders: {0}  Major: {1}", Arr(tp.GetCurrentRowHeaders()).Count, tp.CurrentRowOrColumnMajor);
        }

        // Realized children
        var swc = Stopwatch.StartNew();
        List<IUIAutomationElement> kids;
        try { if (SafeMode) throw new Exception("safe"); kids = Arr(g.FindAll(TreeScope.TreeScope_Children, A.CreateTrueCondition())); }
        catch (Exception ex)
        {
            Console.WriteLine("  FindAll(children) failed after {0} ms: {1:X8}; walking first 300 with TreeWalker", swc.ElapsedMilliseconds, ex.HResult);
            kids = new List<IUIAutomationElement>(); swc.Restart();
            var wk = A.ControlViewWalker; var k = wk.GetFirstChildElement(g);
            while (k != null && kids.Count < 300) { kids.Add(k); k = wk.GetNextSiblingElement(k); }
        }
        var hist = kids.GroupBy(k => Ct(k.CurrentControlType)).Select(gr => gr.Key + "x" + gr.Count());
        Console.WriteLine("  realized children: {0} ({1}) in {2} ms", kids.Count, string.Join(", ", hist), swc.ElapsedMilliseconds);
        var rowKids = kids.Where(k => IsRowCt(k.CurrentControlType)).ToList();
        foreach (var r0 in rowKids.Take(2))
        {
            Console.WriteLine("  row: " + Desc(r0) + " GridItem=" + Has(r0, pGridItemAvail) + " TableItem=" + Has(r0, pTableItemAvail)
                + " Virtualized=" + Has(r0, pVirtAvail) + " offscreen=" + Has(r0, pOffscreen));
            var rc = Arr(r0.FindAll(TreeScope.TreeScope_Children, A.CreateTrueCondition()));
            foreach (var c in rc.Take(8))
            {
                string gi = Has(c, pGridItemAvail) ? string.Format(" GridItem(r={0},c={1})", c.GetCurrentPropertyValue(pGIRow), c.GetCurrentPropertyValue(pGICol)) : " noGridItem";
                string ti = "";
                if (Has(c, pTableItemAvail))
                {
                    var tip = (IUIAutomationTableItemPattern)c.GetCurrentPattern(TableItemP);
                    ti = " TableItem colHdr=[" + string.Join(",", Arr(tip.GetCurrentColumnHeaderItems()).Select(e => Trunc(e.CurrentName, 20))) + "]";
                }
                else ti = " noTableItem";
                Console.WriteLine("    cell: {0} name='{1}' text='{2}'{3}{4}", Ct(c.CurrentControlType), Trunc(c.CurrentName, 30), Trunc(Cell(c)), gi, ti);
            }
        }
        if (rowKids.Count > 0)
        {
            int lastOff = rowKids.Count(k => Has(k, pOffscreen));
            Console.WriteLine("  realized rows offscreen: {0}/{1}; rows w/ VirtualizedItem: {2}", lastOff, rowKids.Count, rowKids.Count(k => Has(k, pVirtAvail)));
        }

        // ItemContainer enumeration (counts all items including virtualized placeholders)
        if (hasIC)
        {
            var ic = (IUIAutomationItemContainerPattern)g.GetCurrentPattern(ItemContainerP);
            var swi = Stopwatch.StartNew(); int n = 0, virt = 0; IUIAutomationElement it = null; string err = "";
            try
            {
                while (n < 5000 && swi.Elapsed.TotalSeconds < 30)
                {
                    it = ic.FindItemByProperty(it, 0, null);
                    if (it == null) break;
                    n++; if (Has(it, pVirtAvail)) virt++;
                }
            }
            catch (Exception ex) { err = " err=" + ex.Message; }
            Console.WriteLine("  ItemContainer walk: {0} items, {1} with VirtualizedItem, {2} ms{3}", n, virt, swi.ElapsedMilliseconds, err);
            // What a virtualized placeholder exposes before Realize
            try
            {
                IUIAutomationElement v = null; it = null;
                for (int i = 0; i < 5000; i++) { it = ic.FindItemByProperty(it, 0, null); if (it == null) break; if (Has(it, pVirtAvail)) { v = it; break; } }
                if (v != null)
                {
                    string kidsInfo; try { kidsInfo = Arr(v.FindAll(TreeScope.TreeScope_Children, A.CreateTrueCondition())).Count.ToString(); } catch (Exception ex) { kidsInfo = "err " + ex.Message; }
                    string nm; try { nm = v.CurrentName; } catch (Exception ex) { nm = "err " + ex.Message; }
                    string ct; try { ct = Ct(v.CurrentControlType); } catch (Exception ex) { ct = "err " + ex.Message; }
                    Console.WriteLine("  virtualized placeholder: ct={0} name='{1}' children={2}", ct, Trunc(nm), kidsInfo);
                }
            }
            catch (Exception ex) { Console.WriteLine("  virtualized placeholder read err: " + ex.Message); }
            if (RealizeMode)
            {
                var swr = Stopwatch.StartNew(); int items = 0, realized = 0, full = 0, rerr = 0; it = null; var sample = new List<string>();
                var req = A.CreateCacheRequest(); req.AddProperty(pName); req.AddProperty(pValue); req.AddProperty(pCT);
                req.TreeScope = TreeScope.TreeScope_Children; req.AutomationElementMode = AutomationElementMode.AutomationElementMode_None;
                while (items < 5000 && swr.Elapsed.TotalSeconds < 120)
                {
                    it = ic.FindItemByProperty(it, 0, null); if (it == null) break; items++;
                    try
                    {
                        if (Has(it, pVirtAvail)) { ((IUIAutomationVirtualizedItemPattern)it.GetCurrentPattern(VirtItemP)).Realize(); realized++; }
                        var c = it.BuildUpdatedCache(req);
                        var cc = Arr(c.GetCachedChildren());
                        if (cc.Count > 0) full++;
                        if (items % 100 == 0 || items == 470 || sample.Count < 2)
                            sample.Add(items + ": " + string.Join(" | ", cc.Select(x => Trunc(S(x.GetCachedPropertyValue(pValue)), 25))));
                    }
                    catch (Exception ex) { rerr++; }
                }
                Console.WriteLine("  REALIZE walk: {0} items, realized {1}, rows with cells {2}, errors {3}, {4} ms", items, realized, full, rerr, swr.ElapsedMilliseconds);
                foreach (var s in sample) Console.WriteLine("      " + s);
                Console.WriteLine("  after realize: Grid RowCount={0}", S(g.GetCurrentPropertyValue(30062)));
            }
        }

        // Method A: GridPattern.GetItem(r,c) for every cell
        if (gp != null && rows > 0 && cols > 0 && !ReadOnlyMode)
        {
            var swa = Stopwatch.StartNew(); int ok = 0, nul = 0, fail = 0, read = 0; string firstErr = null;
            var sample = new List<string>(); var lastRow = "";
            int mismatch = 0;
            for (int r = 0; r < (SafeMode ? Math.Min(rows, 200) : rows) && swa.Elapsed.TotalSeconds < budget; r++)
            {
                var row = new List<string>();
                for (int c = 0; c < cols; c++)
                {
                    read++;
                    try
                    {
                        var e = gp.GetItem(r, c);
                        if (e == null) { nul++; row.Add("<null>"); continue; }
                        row.Add(Trunc(Cell(e), 30)); ok++;
                        if (r % 50 == 0 && Has(e, pGridItemAvail))
                        {
                            int rr = (int)e.GetCurrentPropertyValue(pGIRow), cc = (int)e.GetCurrentPropertyValue(pGICol);
                            if (rr != r || cc != c) mismatch++;
                        }
                    }
                    catch (Exception ex) { fail++; row.Add("<err>"); if (firstErr == null) firstErr = string.Format("r{0}c{1}: {2:X8} {3}", r, c, ex.HResult, ex.Message); }
                }
                if (sample.Count < 4) sample.Add(string.Join(" | ", row));
                lastRow = r + ": " + string.Join(" | ", row);
            }
            Console.WriteLine("  [A] GetItem all cells: {0}/{1} cells attempted, ok={2} null={3} fail={4}, rowcol-mismatch(sampled)={5}, {6} ms ({7:F2} ms/cell){8}",
                read, rows * cols, ok, nul, fail, mismatch, swa.ElapsedMilliseconds, read > 0 ? swa.Elapsed.TotalMilliseconds / read : 0,
                firstErr != null ? " firstErr=" + firstErr : "");
            foreach (var s in sample) Console.WriteLine("      " + s);
            Console.WriteLine("      last " + lastRow);
        }

        if (SafeMode) return; // Method B: one cache request, TreeScope_Subtree, rebuild rows from children
        {
            var req = A.CreateCacheRequest();
            foreach (var p in new[] { pName, pCT, pGIRow, pGICol, pValue, pGridItemAvail, pOffscreen, pVirtAvail }) req.AddProperty(p);
            req.TreeScope = TreeScope.TreeScope_Subtree;
            req.TreeFilter = A.ControlViewCondition;
            req.AutomationElementMode = AutomationElementMode.AutomationElementMode_None;
            var swb = Stopwatch.StartNew();
            IUIAutomationElement cg = null; string err = "";
            try { cg = g.BuildUpdatedCache(req); } catch (Exception ex) { err = ex.Message; }
            long t = swb.ElapsedMilliseconds;
            if (cg != null)
            {
                var ckids = Arr(cg.GetCachedChildren());
                var crow = ckids.Where(k => IsRowCt(k.CachedControlType)).ToList();
                int cells = 0, withGI = 0; var sample = new List<string>();
                foreach (var r in crow)
                {
                    var cc = Arr(r.GetCachedChildren());
                    cells += cc.Count; withGI += cc.Count(x => (bool)x.GetCachedPropertyValue(pGridItemAvail));
                    if (sample.Count < 3)
                        sample.Add(Trunc(r.CachedName, 30) + " :: " + string.Join(" | ", cc.Select(x =>
                        {
                            string n = S(x.GetCachedPropertyValue(pValue)); if (string.IsNullOrEmpty(n) || n.StartsWith("System.")) n = x.CachedName;
                            return Ct(x.CachedControlType) + ":" + Trunc(n, 25);
                        })));
                }
                Console.WriteLine("  [B] cached subtree: {0} ms, {1} row elements, {2} cells under rows ({3} with GridItem)", t, crow.Count, cells, withGI);
                foreach (var s in sample) Console.WriteLine("      " + s);
            }
            else Console.WriteLine("  [B] cached subtree failed: " + err);
        }
    }
}
