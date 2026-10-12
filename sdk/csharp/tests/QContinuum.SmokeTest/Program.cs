// Smoke runner: executes all xUnit [Fact] methods via reflection,
// bypassing vstest (which cannot start testhost in this sandbox).
using System.Reflection;

var testAssembly = typeof(QContinuum.Tests.FramingTests).Assembly;
int passed = 0, failed = 0;
var failures = new List<string>();

foreach (var type in testAssembly.GetTypes())
{
    if (!type.IsClass || type.IsAbstract)
        continue;
    var facts = type.GetMethods(BindingFlags.Public | BindingFlags.Instance)
        .Where(m => m.GetCustomAttributes().Any(a => a.GetType().Name == "FactAttribute"))
        .ToList();
    if (facts.Count == 0)
        continue;

    foreach (var fact in facts)
    {
        var name = $"{type.Name}.{fact.Name}";
        try
        {
            var instance = Activator.CreateInstance(type)!;
            var result = fact.Invoke(instance, null);
            if (result is Task task)
                await task;
            Console.WriteLine($"  PASS {name}");
            passed++;
        }
        catch (Exception ex)
        {
            var inner = ex is TargetInvocationException tie ? tie.InnerException ?? ex : ex;
            Console.WriteLine($"  FAIL {name}: {inner.GetType().Name}: {inner.Message}");
            failures.Add(name);
            failed++;
        }
    }
}

Console.WriteLine($"--- {passed} passed, {failed} failed ---");
return failed == 0 ? 0 : 1;
